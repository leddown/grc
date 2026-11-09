package oscal

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// The grc prop on a statement or guidance part whose prose is grc's own stored
// text rather than OSCAL markup to render. grc's catalog export writes it, and
// the catalog import takes such prose verbatim and leaves the stored statement
// structure as it is, so an export imports back without a change.
const (
	ProseProp      = "prose"
	ProseGRCText   = "grc-text"
	documentDigits = 12
)

func proseProp() Property {
	return Property{Name: ProseProp, Ns: Namespace, Value: ProseGRCText}
}

func isGRCText(props *[]Property) bool {
	if props == nil {
		return false
	}
	for _, p := range *props {
		if p.Name == ProseProp && p.Ns == Namespace && p.Value == ProseGRCText {
			return true
		}
	}
	return false
}

// finishDocument gives a built document its identity and writes it.
//
// The document UUID and, when the export set none, the metadata version are
// derived from the canonical bytes of the document with both left out and
// last-modified zeroed. So two exports of unchanged data have the same UUID,
// version and bytes apart from last-modified, and a change of content gives a
// new UUID, as OSCAL asks of a revised document.
func finishDocument(kind, key string, doc any, uuid *string, meta *Metadata, lastModified time.Time) ([]byte, error) {
	*uuid = ""
	meta.LastModified = time.Time{}
	derivedVersion := meta.Version == ""
	body, err := Canonical(doc)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	*uuid = UUID(kind, key+"\x00"+digest)
	if derivedVersion {
		meta.Version = digest[:documentDigits]
	}
	meta.LastModified = lastModified.UTC().Truncate(time.Second)
	return Canonical(doc)
}

var tokenUnsafe = regexp.MustCompile(`[^a-z0-9._-]+`)

func slug(ref string) string {
	return strings.Trim(tokenUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(ref)), "-"), "-.")
}

// Token turns a grc reference into an OSCAL token (an XML NCName): lower case,
// runs of anything else as one hyphen, and prefix in front when the reference
// does not start with a letter.
func Token(prefix, ref string) string {
	t := slug(ref)
	switch {
	case t == "":
		return prefix
	case t[0] < 'a' || t[0] > 'z':
		return prefix + "-" + t
	}
	return t
}

// ControlToken is the OSCAL id of an SP 800-53 control, as NIST writes it:
// "AC-2" is "ac-2" and "AC-2(1)" is "ac-2.1".
func ControlToken(controlID string) string {
	id := strings.ToLower(strings.TrimSpace(controlID))
	id = strings.ReplaceAll(id, "(", ".")
	return strings.ReplaceAll(id, ")", "")
}

// NFRToken is the OSCAL id of a Security NFR in grc's NFR catalog: "nfr-" and
// its record key.
func NFRToken(key string) string {
	return Token("nfr", "nfr-"+slug(key))
}

// CatalogMediaType is the rlink media type of an OSCAL catalog in JSON, as
// NIST's own profiles give it.
const CatalogMediaType = "application/oscal.catalog+json"

// documentResource is a back-matter resource for another grc export, and the
// "#uuid" reference that points at it. Its UUID is keyed on the href, so the
// same document is the same resource in every export that names it.
func documentResource(title, href string) (Resource, string) {
	r := Resource{
		UUID:   UUID("resource", href),
		Title:  title,
		Rlinks: &[]ResourceLink{{Href: href, MediaType: CatalogMediaType}},
	}
	return r, "#" + r.UUID
}

func backMatter(resources ...Resource) *BackMatter {
	return &BackMatter{Resources: &resources}
}

func grcProp(name, value string) []Property {
	return GRCProps(NamedValue{Name: name, Value: value})
}

func appendProps(dst *[]Property, props ...Property) {
	*dst = append(*dst, props...)
}

func propsOrNil(props []Property) *[]Property {
	if len(props) == 0 {
		return nil
	}
	return &props
}

func partsOrNil(parts []Part) *[]Part {
	if len(parts) == 0 {
		return nil
	}
	return &parts
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
