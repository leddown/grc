package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// A partial exception can be described on its card of the Exceptions page.
// The description is kept in the browser for the tab's session, beside the
// exercise and not in the address, and the application is sent it only in the
// body of a request for a workbook.
func TestExceptionsPageDescribesPartialExceptions(t *testing.T) {
	router := newNavigationTestRouter(t)

	page := getPageBody(t, router, "/exceptions")
	for _, want := range []string{
		"const DESCRIPTIONS_SESSION_KEY = 'exceptions.descriptions';",
		"const DESCRIPTION_MAX = " + strconv.Itoa(descriptionMaxRunes) + ";",
		`'<label class="exception-description"' + (state.partial.has(key.trim()) ? '' : ' hidden') + '>'`,
		`<textarea class="exception-note" data-key="`,
		"description.hidden = kind.value !== 'partial';",
		"state.descriptions.set(String(note.getAttribute('data-key') || '').trim(), cleanDescription(note.value));",
		"state.descriptions = storedDescriptions();",
		"dropDescriptionsWithoutException();\n      storeDescriptions(state.descriptions);",
		"window.sessionStorage.setItem(DESCRIPTIONS_SESSION_KEY, JSON.stringify(kept));",
		".exception-description[hidden] { display: none; }",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("/exceptions is missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"params.append('description",
		"localStorage.setItem(DESCRIPTIONS_SESSION_KEY",
		"form.submit()",
		"method: 'POST'",
	} {
		if strings.Contains(page, unwanted) {
			t.Errorf("/exceptions has %q: a description stays in the tab's session and out of the address", unwanted)
		}
	}

	workbook := getPageBody(t, router, "/reports/exceptions-workbook")
	for _, want := range []string{
		"const sent = descriptionsToSend(storedDescriptions(), partialFromAddress(stored));",
		"if (!sent.length) return;",
		"form.method = 'post';",
		"form.action = sessionDownload.href;",
		"field.name = '" + descriptionField + "' + key;",
		"function descriptionsToSend(descriptions, partial)",
	} {
		if !strings.Contains(workbook, want) {
			t.Errorf("/reports/exceptions-workbook is missing %q", want)
		}
	}
	if strings.Contains(workbook, "sessionDownload.href +") || strings.Contains(workbook, "'&description") {
		t.Error("the descriptions must go in the body of the request, not in its address")
	}
}

// What is said of a partial exception comes in the body of a POST and is that
// NFR's cell in the Comments column. One for anything but a partial exception
// of the request is left out, and a body that is anything but descriptions is
// refused whole.
func TestExceptionsWorkbookDownloadTakesDescriptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	conn := sqliteNoopDB(t)
	userHandler, _, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler, err := buildHandlers(conn)
	if err != nil {
		t.Fatalf("buildHandlers: %v", err)
	}
	router := gin.New()
	registerRoutes(router, userHandler, authHandler, controlHandler, nfrHandler, linkHandler, reportsHandler, riskHandler,
		noAuthMiddleware, false, true)

	nfrs, err := nfrHandler.Service().List("", "")
	if err != nil || len(nfrs) < 4 {
		t.Fatalf("no seeded NFRs: %v", err)
	}
	full, partial, other := nfrs[0].Key, nfrs[1].Key, nfrs[2].Key
	query := "?" + url.Values{"key": {full, partial}, "partial": {partial}}.Encode()

	send := func(method, query, contentType, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/reports/exceptions-workbook/download"+query, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	const form = "application/x-www-form-urlencoded"
	// The Exceptions sheet is the second one of the workbook.
	sheet := func(rec *httptest.ResponseRecorder) string {
		t.Helper()
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != xlsxContentType {
			t.Fatalf("status=%d Content-Type=%q body=%.200s", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
		}
		archive, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil {
			t.Fatalf("the download is not a zip: %v", err)
		}
		for _, entry := range archive.File {
			if entry.Name != "xl/worksheets/sheet2.xml" {
				continue
			}
			reader, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(reader)
			_ = reader.Close()
			if err != nil {
				t.Fatal(err)
			}
			return string(raw)
		}
		t.Fatal("the workbook has no second sheet")
		return ""
	}
	// The Comments column is H; a text cell is an inline string, not a formula.
	comment := func(sheet, escaped string) (cell, row string, found bool) {
		match := regexp.MustCompile(`<c r="H(\d+)"[^>]*t="inlineStr"><is><t xml:space="preserve">` + regexp.QuoteMeta(escaped) + `</t></is></c>`).FindStringSubmatch(sheet)
		if match == nil {
			return "", "", false
		}
		return match[0], match[1], true
	}

	// As a browser sends it: line ends as CR LF. The bell is a control
	// character no cell can hold, and the text starts like a formula.
	said := "=Filtered at the edge, not between zones.\r\nReview due in March: <b> & \"more\".\a"
	wanted := "=Filtered at the edge, not between zones.\nReview due in March: &lt;b&gt; &amp; &quot;more&quot;."
	described := sheet(send(http.MethodPost, query, form, url.Values{
		descriptionField + partial:       {"  " + said + "  "},
		descriptionField + full:          {"said of a full exception"},
		descriptionField + other:         {"said of an NFR that is not excepted"},
		descriptionField + "no-such-key": {"said of an NFR the catalog does not have"},
	}.Encode()))
	cell, row, found := comment(described, wanted)
	if !found {
		t.Fatal("the description of the partial exception is not a text cell of the Comments column")
	}
	if !regexp.MustCompile(`<c r="B` + row + `"[^>]*><is><t[^>]*>Partial</t>`).MatchString(described) {
		t.Errorf("row %s has the description and is not the partial exception", row)
	}
	if strings.Count(described, "Filtered at the edge") != 1 {
		t.Error("the description is in the sheet more than once")
	}
	if strings.Contains(described, "said of") {
		t.Error("a description for something that is not a partial exception is in the workbook")
	}
	if strings.Contains(described, "<f>=Filtered") || strings.Contains(described, "<f>Filtered") {
		t.Error("a description is written as a formula")
	}

	// Without the descriptions it is the same sheet but for that one cell,
	// which is empty and so not written.
	plain := sheet(send(http.MethodGet, query, "", ""))
	if strings.Contains(plain, "Filtered at the edge") {
		t.Fatal("a download without descriptions has one")
	}
	if strings.Replace(described, cell, "", 1) != plain {
		t.Error("the description changed more of the sheet than its own cell")
	}
	if empty := sheet(send(http.MethodPost, query, form, "")); empty != plain {
		t.Error("a POST with nothing in its body is not the workbook a GET gives")
	}
	// A description is never read from the address.
	inAddress := query + "&" + url.Values{descriptionField + partial: {said}}.Encode()
	for _, rec := range []*httptest.ResponseRecorder{send(http.MethodGet, inAddress, "", ""), send(http.MethodPost, inAddress, form, "")} {
		if strings.Contains(sheet(rec), "Filtered at the edge") {
			t.Error("a description was read from the address")
		}
	}

	// A description of the longest length, in characters of more than one
	// byte, with what is cleaned away around it.
	longest := strings.Repeat("é", descriptionMaxRunes)
	if _, _, found := comment(sheet(send(http.MethodPost, query, form, url.Values{descriptionField + partial: {" \x00" + longest + "\x7f\r\n"}}.Encode())), longest); !found {
		t.Errorf("a description of %d characters is not in the workbook", descriptionMaxRunes)
	}

	field := url.QueryEscape(descriptionField + partial)
	for name, request := range map[string]struct{ contentType, body string }{
		"no content type":            {"", field + "=x"},
		"JSON":                       {"application/json", `{"` + descriptionField + partial + `":"x"}`},
		"a multipart form":           {"multipart/form-data; boundary=x", "--x\r\nContent-Disposition: form-data; name=\"" + descriptionField + partial + "\"\r\n\r\nx\r\n--x--\r\n"},
		"a field of another name":    {form, field + "=x&key=" + url.QueryEscape(other)},
		"a selection in the body":    {form, "partial=" + url.QueryEscape(other)},
		"a field with no key":        {form, url.QueryEscape(descriptionField) + "=x"},
		"a key with space around it": {form, url.QueryEscape(descriptionField+" "+partial) + "=x"},
		"a key with a control":       {form, url.QueryEscape(descriptionField+partial+"\n") + "=x"},
		"a key too long":             {form, url.QueryEscape(descriptionField+strings.Repeat("k", descriptionMaxKey+1)) + "=x"},
		"a field given twice":        {form, field + "=x&" + field + "=y"},
		"a description too long":     {form, field + "=" + strings.Repeat("x", descriptionMaxRunes+1)},
		"text that is not UTF-8":     {form, field + "=%ff%fe"},
		"a body that is not a form":  {form, field + "=%zz"},
		"a body too large":           {form, field + "=" + strings.Repeat("%20", descriptionMaxBody/3+1)},
	} {
		rec := send(http.MethodPost, query, request.contentType, request.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status=%d, want %d", name, rec.Code, http.StatusBadRequest)
		}
		if strings.HasPrefix(rec.Body.String(), "PK") || rec.Header().Get("Content-Disposition") != "" {
			t.Errorf("%s: a workbook was sent", name)
		}
	}
}

func TestCleanDescription(t *testing.T) {
	for _, c := range []struct {
		text, want string
		ok         bool
	}{
		{"", "", true},
		{"  plain  ", "plain", true},
		{"one\r\ntwo\rthree\nfour", "one\ntwo\nthree\nfour", true},
		{"tab\tkept", "tab\tkept", true},
		{"bell\a, escape\x1b, delete\x7f, next line\u0085, null\x00.", "bell, escape, delete, next line, null.", true},
		{"\x00\x01 \r\n", "", true},
		{"=SUM(A1:A9)", "=SUM(A1:A9)", true},
		{"😀 née", "😀 née", true},
		{"not \xff UTF-8", "", false},
		{strings.Repeat("字", descriptionMaxRunes), strings.Repeat("字", descriptionMaxRunes), true},
		{strings.Repeat("字", descriptionMaxRunes+1), "", false},
		{" " + strings.Repeat("x", descriptionMaxRunes) + "\a\n", strings.Repeat("x", descriptionMaxRunes), true},
	} {
		got, ok := cleanDescription(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("cleanDescription(%.40q) = %.40q, %v; want %.40q, %v", c.text, got, ok, c.want, c.ok)
		}
	}
}

// descriptionsHarness runs after the pages' script and a line that sets
// texts. It prints what the pages would send for each text, and whether the
// rest of what they do with descriptions holds.
const descriptionsHarness = `
const kept = new Map();
const window = { sessionStorage: {
  getItem: (key) => kept.has(key) ? kept.get(key) : null,
  setItem: (key, value) => { kept.set(key, String(value)); },
  removeItem: (key) => { kept.delete(key); }
} };

const failures = [];
const check = (name, ok, detail) => { if (!ok) failures.push(name + (detail ? ': ' + detail : '')); };

// What the pages send for a text: it alone, as the description of a partial exception.
const sent = texts.map((text) => {
  const pairs = descriptionsToSend(new Map([['k', text]]), new Set(['k']));
  return pairs.length ? pairs[0][1] : '';
});

const descriptions = new Map([['1', 'of a partial exception'], ['2', 'of a full exception'], ['3', '   '], ['4', 'another\npartial one']]);
check('only the partial exceptions are sent',
  JSON.stringify(descriptionsToSend(descriptions, new Set(['1', '3', '4', '5']))) === '[["1","of a partial exception"],["4","another\\npartial one"]]',
  JSON.stringify(descriptionsToSend(descriptions, new Set(['1', '3', '4', '5']))));
check('nothing is sent when nothing is partial', descriptionsToSend(descriptions, new Set()).length === 0);

storeDescriptions(descriptions);
check('the descriptions are kept under their own name', kept.size === 1 && kept.has(DESCRIPTIONS_SESSION_KEY) && DESCRIPTIONS_SESSION_KEY !== EXERCISE_SESSION_KEY);
check('an empty description is not kept', kept.get(DESCRIPTIONS_SESSION_KEY) === '{"1":"of a partial exception","2":"of a full exception","4":"another\\npartial one"}', kept.get(DESCRIPTIONS_SESSION_KEY));
const back = storedDescriptions();
check('what is kept comes back', back.size === 3 && back.get('4') === 'another\npartial one' && back.get('2') === 'of a full exception');
storeDescriptions(new Map());
check('with no descriptions nothing is kept', kept.size === 0);
check('nothing kept is no descriptions', storedDescriptions().size === 0);

// Whatever else is found under the name is not a description.
for (const found of ['not JSON', '[]', '"text"', 'null', '7', '{"1":7,"2":null,"3":["x"],"4":{"a":"b"}}']) {
  kept.set(DESCRIPTIONS_SESSION_KEY, found);
  check('kept ' + found + ' gives no descriptions', storedDescriptions().size === 0, String(storedDescriptions().size));
}
kept.set(DESCRIPTIONS_SESSION_KEY, JSON.stringify({ ' 9 ': 'x'.repeat(DESCRIPTION_MAX + 50) + '\u0007', '': 'no key', '8': '  ' }));
const cut = storedDescriptions();
check('what is kept is cleaned as it is read', cut.size === 1 && cut.get('9') === 'x'.repeat(DESCRIPTION_MAX), JSON.stringify(Array.from(cut.keys())));

console.log(JSON.stringify({ sent, failures }));
`

// TestPageDescriptionsUnderNode runs what the pages do with descriptions under
// node, and holds the page to the application: what the page sends for a text
// is what the application takes, unchanged. It needs node on PATH and is
// skipped without it.
func TestPageDescriptionsUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node on PATH: the pages' script is not run")
	}

	texts := []string{
		"",
		"   ",
		"Filtered at the edge, not between zones.",
		"  space around it \n",
		"one\r\ntwo\rthree\nfour\ttab",
		"bell\a, escape\x1b, delete\x7f, next line\u0085, null\x00.",
		"\u0085\u00a0\ufeff ends that only one side calls space \ufeff\u00a0\u0085",
		"=HYPERLINK(\"http://example.test\",\"a formula?\")",
		"<script>alert(1)</script> & \"quotes\" 'too'",
		"😀 née 字",
		strings.Repeat("x", descriptionMaxRunes),
		strings.Repeat("x", descriptionMaxRunes+200),
		strings.Repeat("😀", descriptionMaxRunes+5),
		strings.Repeat(" ", 30) + strings.Repeat("字", descriptionMaxRunes+5),
		strings.Repeat("a\r\n", descriptionMaxRunes),
	}
	raw, err := json.Marshal(texts)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "descriptions.js")
	script := exceptionScopeScript + "\nconst texts = " + string(raw) + ";\n" + descriptionsHarness
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	output, err := exec.CommandContext(ctx, node, path).Output()
	if err != nil {
		t.Fatalf("the pages' script failed under node: %v\n%s", err, output)
	}
	var onPage struct {
		Sent     []string `json:"sent"`
		Failures []string `json:"failures"`
	}
	if err := json.Unmarshal(output, &onPage); err != nil || len(onPage.Sent) != len(texts) {
		t.Fatalf("the pages' script gave %d texts, want %d: %v\n%.300s", len(onPage.Sent), len(texts), err, output)
	}
	for _, failure := range onPage.Failures {
		t.Errorf("the pages' script: %s", failure)
	}

	for i, sent := range onPage.Sent {
		taken, ok := cleanDescription(sent)
		if !ok || taken != sent {
			t.Errorf("text %d: the page sends %.60q and the application takes %.60q (%v)", i, sent, taken, ok)
		}
		if utf8.RuneCountInString(sent) > descriptionMaxRunes {
			t.Errorf("text %d: the page sends %d characters", i, utf8.RuneCountInString(sent))
		}
		// A text within the length is cleaned by the two to the same thing,
		// but for the ends: the page takes a byte order mark there for space.
		if whole, ok := cleanDescription(texts[i]); ok && strings.Trim(whole, "\ufeff\u00a0 ") != strings.Trim(sent, "\ufeff\u00a0 ") {
			t.Errorf("text %d: the page cleans it to %.60q and the application to %.60q", i, sent, whole)
		}
	}
	if onPage.Sent[2] != texts[2] || onPage.Sent[0] != "" || onPage.Sent[1] != "" {
		t.Errorf("the page sends %q, %q, %q for the first three texts", onPage.Sent[0], onPage.Sent[1], onPage.Sent[2])
	}
}
