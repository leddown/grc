package policystudio

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"grc/internal/policydocs"
)

func guestReq(token, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://grc.example.com/collab/policies/1", nil)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: GuestCookie, Value: token})
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

func TestShareLinksAreRefusedWhenOffOrLocal(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc(sixSections...)
	f.studio.guestLinks = func() bool { return false }
	var ga ErrGuestAccess
	if _, _, err := f.studio.CreateShareLink(doc.ID, NewShareLink{}, "alice"); !errors.As(err, &ga) || !strings.Contains(ga.Msg, "switched off") {
		t.Fatalf("with guest links off: %v", err)
	}
	f.studio.guestLinks = func() bool { return true }
	f.studio.localMode = true
	if _, _, err := f.studio.CreateShareLink(doc.ID, NewShareLink{}, "alice"); !errors.As(err, &ga) || !strings.Contains(ga.Msg, "local mode") {
		t.Fatalf("in local mode: %v", err)
	}
	if _, _, err := f.studio.Redeem("x", "Bob"); !errors.As(err, &ga) {
		t.Fatalf("redeeming in local mode: %v", err)
	}
}

func TestGuestLinkLifecycle(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc(sixSections...)
	other := f.studioDoc(sixSections...)

	for name, in := range map[string]NewShareLink{
		"bad role":     {Role: "suggester"},
		"too long":     {ExpiresHours: 24*7 + 1},
		"too many":     {MaxUses: 501},
		"negative use": {MaxUses: -1},
	} {
		if _, _, err := f.studio.CreateShareLink(doc.ID, in, "alice"); !policydocs.IsValidation(err) {
			t.Errorf("%s: %v", name, err)
		}
	}
	link, token, err := f.studio.CreateShareLink(doc.ID, NewShareLink{Label: "CISO workshop", Role: RoleEditor, MaxUses: 1}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 43 || link.Status != "active" || link.MaxUses != 1 {
		t.Fatalf("link %+v token %q", link, token)
	}
	var stored int
	_ = f.conn.QueryRow(`SELECT COUNT(*) FROM policy_share_links WHERE token_sha256 = ?`, token).Scan(&stored)
	if stored != 0 {
		t.Fatal("the token itself is stored")
	}
	expiry := f.studio.parseStamp(link.ExpiresAt).Sub(f.studio.parseStamp(link.CreatedAt))
	if expiry != 8*time.Hour {
		t.Fatalf("default expiry %v", expiry)
	}

	if _, _, err := f.studio.Redeem(token, "   "); !policydocs.IsValidation(err) {
		t.Fatalf("an empty name: %v", err)
	}
	g, session, err := f.studio.Redeem(token, "  Carla\x00  Mendes ")
	if err != nil {
		t.Fatal(err)
	}
	if g.DisplayName != "Carla Mendes" || g.Role != RoleEditor || g.DocumentID != doc.ID {
		t.Fatalf("session %+v", g)
	}
	var ga ErrGuestAccess
	if _, _, err := f.studio.Redeem(token, "Someone else"); !errors.As(err, &ga) || ga.Status != http.StatusGone {
		t.Fatalf("a used-up link: %v", err)
	}
	if got, ok := f.studio.Guest(guestReq(session, "")); !ok || got.ID != g.ID {
		t.Fatalf("the session cookie resolves: %+v %v", got, ok)
	}

	const own = "https://grc.example.com"
	for _, tc := range []struct {
		name     string
		r        *http.Request
		doc      int64
		allow    bool
		readOnly bool
	}{
		{"editor guest, its document", guestReq(session, own), doc.ID, true, false},
		{"editor guest, another document", guestReq(session, own), other.ID, false, false},
		{"editor guest, cross origin", guestReq(session, "https://evil.example"), doc.ID, false, false},
		{"no cookie", guestReq("", own), doc.ID, false, false},
		{"forged cookie", guestReq(strings.Repeat("A", 43), own), doc.ID, false, false},
	} {
		d := f.studio.decide(tc.r, tc.doc)
		if d.Allow != tc.allow || d.Allow && d.ReadOnly != tc.readOnly {
			t.Errorf("%s: %+v", tc.name, d)
		}
	}
	viewerLink, viewerToken, _ := f.studio.CreateShareLink(doc.ID, NewShareLink{Role: RoleViewer}, "alice")
	_, viewerSession, err := f.studio.Redeem(viewerToken, "Viv")
	if err != nil {
		t.Fatal(err)
	}
	if d := f.studio.decide(guestReq(viewerSession, own), doc.ID); !d.Allow || !d.ReadOnly {
		t.Fatalf("a viewer is read-only: %+v", d)
	}

	sharing, err := f.studio.Sharing(doc.ID)
	if err != nil || len(sharing.Links) != 2 || len(sharing.Guests) != 2 || !sharing.Enabled {
		t.Fatalf("sharing: %+v %v", sharing, err)
	}

	if err := f.studio.KickGuest(doc.ID, g.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.studio.Guest(guestReq(session, "")); ok {
		t.Fatal("a removed guest's session still works")
	}
	if err := f.studio.RevokeShareLink(doc.ID, viewerLink.ID, "alice"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.studio.Guest(guestReq(viewerSession, "")); ok {
		t.Fatal("a revoked link's session still works")
	}
	if _, _, err := f.studio.Redeem(viewerToken, "Viv again"); !errors.As(err, &ga) || !strings.Contains(ga.Msg, "withdrawn") {
		t.Fatalf("redeeming a revoked link: %v", err)
	}
	if err := f.studio.KickGuest(other.ID, g.ID, "alice"); !errors.Is(err, policydocs.ErrNotFound) {
		t.Fatalf("a guest is removed only through their own document: %v", err)
	}
}

// An expired link is refused, and a session that outlives its link's expiry is
// ended by the sweep, not at the guest's next request.
func TestGuestExpiry(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc(sixSections...)
	_, token, err := f.studio.CreateShareLink(doc.ID, NewShareLink{Role: RoleCommenter, ExpiresHours: 1}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	_, session, err := f.studio.Redeem(token, "Carla")
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Hour)
	f.studio.now = func() time.Time { return later }
	f.studio.store.now = f.studio.now
	var ga ErrGuestAccess
	if _, _, err := f.studio.Redeem(token, "Late"); !errors.As(err, &ga) || !strings.Contains(ga.Msg, "expired") {
		t.Fatalf("an expired link: %v", err)
	}
	if _, ok := f.studio.Guest(guestReq(session, "")); ok {
		t.Fatal("an expired session still works")
	}
	f.studio.sweepGuests()
	var ended, audited int
	_ = f.conn.QueryRow(`SELECT COUNT(*) FROM policy_guest_sessions WHERE end_reason = 'expired'`).Scan(&ended)
	_ = f.conn.QueryRow(`SELECT COUNT(*) FROM policy_studio_audit WHERE event = ?`, EventGuestEnded).Scan(&audited)
	if ended != 1 || audited != 1 {
		t.Fatalf("sweep ended %d, audited %d", ended, audited)
	}
	// Purged thirty days after it expired.
	f.studio.now = func() time.Time { return later.Add(31 * 24 * time.Hour) }
	f.studio.store.now = f.studio.now
	f.studio.sweepGuests()
	var left int
	_ = f.conn.QueryRow(`SELECT COUNT(*) FROM policy_guest_sessions`).Scan(&left)
	if left != 0 {
		t.Fatalf("%d sessions kept past retention", left)
	}
}

// What a guest is sent: the text's structure and the facts it uses, and none of
// the internal review -- not lint, mappings, guidance or internal threads, and
// not even a change of version when an internal thread is added.
func TestGuestStateAndThreads(t *testing.T) {
	f := newStudio(t)
	doc := f.studioDoc(sixSections...)
	uid := f.sections(doc.ID)[0].UID
	if _, err := f.conn.Exec(`INSERT INTO rcsa_controls (control_id, name, family, mapping_baselines_json, threats_json) VALUES ('AC-5', 'Separation of Duties', 'AC', '[]', '[]')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.policies.AttachControl(doc.ID, f.sections(doc.ID)[0].ID, policydocs.ControlRef{ControlID: "AC-5", Coverage: "partial"}); err != nil {
		t.Fatal(err)
	}
	_, token, _ := f.studio.CreateShareLink(doc.ID, NewShareLink{Role: RoleCommenter}, "alice")
	g, _, err := f.studio.Redeem(token, "Carla")
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.studio.GuestState(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Findings) != 0 || st.CanEdit || st.CanDecide || !st.CanComment || st.Guest == nil || st.Guest.Name != "Carla" || st.Document.Author != "" {
		t.Fatalf("guest state: %+v", st)
	}
	for _, sec := range st.Sections {
		if len(sec.Controls) != 0 || sec.Guidance != "" || sec.ProvDetails != "" {
			t.Fatalf("a guest sees internal section detail: %+v", sec)
		}
	}
	before := st.ReviewVersion
	internal, err := f.studio.CreateThread(doc.ID, NewThread{SectionUID: uid, AnchorStart: anchor(1, 2), AnchorEnd: anchor(1, 3), Body: "INTERNAL-ONLY"}, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if st, _ = f.studio.GuestState(g); st.ReviewVersion != before {
		t.Fatal("an internal thread changed the guest's review version")
	}
	if _, err := f.studio.reply(doc.ID, internal.ID, "Can I see this?", g.Actor(), AuthorGuest, true); !errors.Is(err, policydocs.ErrNotFound) {
		t.Fatalf("a guest replied to an internal thread: %v", err)
	}
	shared, err := f.studio.createThread(doc.ID, NewThread{SectionUID: uid, AnchorStart: anchor(1, 2), AnchorEnd: anchor(1, 3), Body: "From the client", Visibility: VisibilityShared},
		ThreadComment, "", g.Actor(), g.Actor(), AuthorGuest)
	if err != nil {
		t.Fatal(err)
	}
	if shared.Comments[0].AuthorKind != AuthorGuest || shared.Comments[0].Author != "Carla (guest)" {
		t.Fatalf("guest comment: %+v", shared.Comments[0])
	}
	if st, _ = f.studio.GuestState(g); st.ReviewVersion == before {
		t.Fatal("a shared thread did not change the guest's review version")
	}
}
