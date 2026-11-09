package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"grc/internal/controlcatalog"
	"grc/internal/nfrlink"
	"grc/internal/oscal"
	"grc/internal/regcoverage"
	"grc/internal/securitynfr"
)

// The OSCAL exports. Each reads through its module's service and hands
// internal/oscal neutral input, so no domain module imports an OSCAL library.
// They are read-only and open to any signed-in user, under the prefixes the
// module's pages already sit under.

// Where the exports are served. Documents that refer to one another (a
// profile to its catalog, a mapping to both ends) use these root-relative
// hrefs, which resolve against the server that served them.
const (
	oscalCatalogPath    = "/controls/oscal/catalog.json"
	oscalNFRCatalogPath = "/security-nfrs/oscal/catalog.json"
	oscalNFRMappingPath = "/security-nfrs/oscal/mapping.json"
)

func oscalProfilePath(baseline string) string {
	return "/controls/oscal/profile/" + baseline + ".json"
}

func oscalRegulationPath(id int64, doc string, version int) string {
	return fmt.Sprintf("/regulation-coverage/%d/oscal/%s.json?version=%d", id, doc, version)
}

const (
	oscalCatalogTitle    = "grc: NIST SP 800-53 control catalog"
	oscalNFRCatalogTitle = "grc: Security NFR catalog"
	oscalNFRMappingTitle = "grc: Security NFRs to NIST SP 800-53"
)

type oscalExports struct {
	controls *controlcatalog.Service
	nfrs     *securitynfr.Service
	links    *nfrlink.Service
	now      func() time.Time
}

func newOSCALExports(controls *controlcatalog.Service, nfrs *securitynfr.Service, links *nfrlink.Service) *oscalExports {
	return &oscalExports{controls: controls, nfrs: nfrs, links: links, now: time.Now}
}

// writeOSCAL sends an export as a download named grc-<kind>-<yyyy-mm-dd>.oscal.json,
// dated by the document's last-modified.
func writeOSCAL(c *gin.Context, kind string, lastModified time.Time, body []byte) {
	name := "grc-" + kind + "-" + lastModified.UTC().Format("2006-01-02") + ".oscal.json"
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

func oscalFailed(c *gin.Context, what string, err error) {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to export " + what + ": " + err.Error()})
}

func (e *oscalExports) exportControls() ([]oscal.ExportControl, error) {
	controls, err := e.controls.ListWithOSCAL()
	if err != nil {
		return nil, err
	}
	out := make([]oscal.ExportControl, 0, len(controls))
	for _, c := range controls {
		out = append(out, oscal.ExportControl{
			ID:             c.ID,
			Name:           c.Name,
			Family:         c.Family,
			Requirements:   c.Requirements,
			Discussion:     c.Discussion,
			Related:        c.RelatedControls,
			Withdrawn:      c.Withdrawn,
			ParamsJSON:     string(c.Params),
			StatementJSON:  string(c.Statement),
			AssessmentJSON: string(c.Assessment),
		})
	}
	return out, nil
}

// catalog serves GET /controls/oscal/catalog.json.
func (e *oscalExports) catalog(c *gin.Context) {
	controls, err := e.exportControls()
	if err != nil {
		oscalFailed(c, "the catalog", err)
		return
	}
	if len(controls) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "the control catalog is empty: import it first"})
		return
	}
	families := map[string]string{}
	for _, ctl := range controls {
		families[ctl.Family] = controlcatalog.FamilyName(ctl.Family)
	}
	now := e.now()
	body, err := oscal.ExportCatalog(oscal.CatalogExport{Title: oscalCatalogTitle, LastModified: now, Families: families, Controls: controls})
	if err != nil {
		oscalFailed(c, "the catalog", err)
		return
	}
	writeOSCAL(c, "sp800-53-catalog", now, body)
}

// profile serves GET /controls/oscal/profile/:baseline.json.
func (e *oscalExports) profile(c *gin.Context) {
	baseline := strings.ToLower(strings.TrimSuffix(c.Param("file"), ".json"))
	if !isOSCALBaseline(baseline) || !strings.HasSuffix(c.Param("file"), ".json") {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such baseline: use low.json, moderate.json, high.json or privacy.json"})
		return
	}
	controls, err := e.controls.ListWithOSCAL()
	if err != nil {
		oscalFailed(c, "the profile", err)
		return
	}
	var ids []string
	for _, ctl := range controls {
		for _, b := range ctl.Baselines {
			if strings.EqualFold(b, baseline) {
				ids = append(ids, ctl.ID)
				break
			}
		}
	}
	now := e.now()
	body, err := oscal.ExportProfile(oscal.ProfileExport{
		Baseline:     baseline,
		Title:        "grc: NIST SP 800-53 " + strings.ToUpper(baseline) + " baseline",
		LastModified: now,
		CatalogHref:  oscalCatalogPath,
		ControlIDs:   ids,
	})
	if errors.Is(err, oscal.ErrEmptyBaseline) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no control in the catalog is in the " + strings.ToUpper(baseline) + " baseline"})
		return
	}
	if err != nil {
		oscalFailed(c, "the profile", err)
		return
	}
	writeOSCAL(c, "sp800-53-"+baseline+"-profile", now, body)
}

func isOSCALBaseline(b string) bool {
	for _, known := range oscal.Baselines {
		if b == known {
			return true
		}
	}
	return false
}

func (e *oscalExports) exportNFRs() ([]oscal.ExportNFR, error) {
	nfrs, err := e.nfrs.List("", "")
	if err != nil {
		return nil, err
	}
	out := make([]oscal.ExportNFR, 0, len(nfrs))
	for _, n := range nfrs {
		out = append(out, oscal.ExportNFR{
			Key:               n.Key,
			ID:                n.ID,
			Summary:           n.Summary,
			IssueType:         n.IssueType,
			Description:       n.Description,
			NISTMapping:       n.NISTMapping,
			AdditionalDetails: n.AdditionalDetails,
			Implementation:    n.Implementation,
			Domain:            n.Domain,
			Weight:            n.Weight,
		})
	}
	return out, nil
}

// nfrCatalog serves GET /security-nfrs/oscal/catalog.json.
func (e *oscalExports) nfrCatalog(c *gin.Context) {
	nfrs, err := e.exportNFRs()
	if err != nil {
		oscalFailed(c, "the NFR catalog", err)
		return
	}
	if len(nfrs) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "the Security NFR catalog is empty: import it first"})
		return
	}
	now := e.now()
	body, err := oscal.ExportNFRCatalog(oscal.NFRCatalogExport{Title: oscalNFRCatalogTitle, LastModified: now, NFRs: nfrs})
	if err != nil {
		oscalFailed(c, "the NFR catalog", err)
		return
	}
	writeOSCAL(c, "nfr-catalog", now, body)
}

// nfrMapping serves GET /security-nfrs/oscal/mapping.json.
func (e *oscalExports) nfrMapping(c *gin.Context) {
	nfrs, err := e.exportNFRs()
	if err != nil {
		oscalFailed(c, "the NFR mapping", err)
		return
	}
	rows, err := e.links.List("", false)
	if err != nil {
		oscalFailed(c, "the NFR mapping", err)
		return
	}
	controls, err := e.controls.ListWithOSCAL()
	if err != nil {
		oscalFailed(c, "the NFR mapping", err)
		return
	}
	links := make([]oscal.ExportNFRLink, 0, len(rows))
	for _, r := range rows {
		control := ""
		if r.Matched {
			control = r.ControlID
		}
		links = append(links, oscal.ExportNFRLink{NFRKey: r.NFRKey, WrittenAs: r.MappingControlID, ControlID: control, Override: r.OverrideApplied})
	}
	ids := make([]string, 0, len(controls))
	for _, ctl := range controls {
		ids = append(ids, ctl.ID)
	}
	now := e.now()
	body, err := oscal.ExportNFRMapping(oscal.NFRMappingExport{
		Title:        oscalNFRMappingTitle,
		LastModified: now,
		NFRs:         nfrs,
		Links:        links,
		ControlIDs:   ids,
		SourceHref:   oscalNFRCatalogPath,
		TargetHref:   oscalCatalogPath,
	})
	if errors.Is(err, oscal.ErrNothingMapped) {
		c.JSON(http.StatusNotFound, gin.H{"error": "no Security NFR is linked to a control in the catalog"})
		return
	}
	if err != nil {
		oscalFailed(c, "the NFR mapping", err)
		return
	}
	writeOSCAL(c, "nfr-mapping", now, body)
}

// regulationOSCAL serves GET /regulation-coverage/:id/oscal/catalog.json and
// mapping.json (?version=N, the latest without it). Both read the version's
// snapshot, never the live findings, and are dated by the version.
func regulationOSCAL(service *regcoverage.Service, doc string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := strconv.ParseInt(c.Param("id"), 10, 64)
		if err != nil || id <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid regulation id"})
			return
		}
		version, _ := strconv.Atoi(strings.TrimSpace(c.Query("version")))
		report, err := service.VersionReport(id, version)
		var notFound regcoverage.ErrNotFound
		if errors.As(err, &notFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error() + ": the regulation has no analysed version to export"})
			return
		}
		if err != nil {
			oscalFailed(c, "the regulation", err)
			return
		}
		latest, err := service.LatestVersionNumber(id)
		if err != nil {
			oscalFailed(c, "the regulation", err)
			return
		}
		reg := exportRegulation(report, latest)
		var body []byte
		switch doc {
		case "catalog":
			body, err = oscal.ExportRegulationCatalog(reg)
		default:
			body, err = oscal.ExportRegulationMapping(reg, oscal.RegulationHrefs{
				Catalog:        oscalRegulationPath(id, "catalog", reg.Version),
				NFRCatalog:     oscalNFRCatalogPath,
				ControlCatalog: oscalCatalogPath,
			})
		}
		if errors.Is(err, oscal.ErrNothingMapped) {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("version %d of this report maps no section to a catalog item", reg.Version)})
			return
		}
		if err != nil {
			oscalFailed(c, "the regulation", err)
			return
		}
		writeOSCAL(c, fmt.Sprintf("regulation-%d-v%d-%s", id, reg.Version, doc), reg.LastModified, body)
	}
}

func exportRegulation(report *regcoverage.Report, latest int) oscal.ExportRegulation {
	r := report.Regulation
	created, _ := time.Parse(time.RFC3339, report.Version.CreatedAt)
	out := oscal.ExportRegulation{
		ID:            r.ID,
		Title:         r.Title,
		Framework:     r.Framework,
		FrameworkName: r.FrameworkName,
		SourceRef:     r.SourceRef,
		TextSHA256:    r.SHA256,
		Version:       report.Version.Number,
		Latest:        report.Version.Number == latest,
		VersionNote:   report.Version.Note,
		LastModified:  created,
	}
	for _, e := range report.Sections {
		s := oscal.ExportSection{
			Ref:               e.Section.Ref,
			Label:             e.Section.Label,
			Title:             e.Section.Title,
			Category:          e.Section.Category,
			Body:              e.Section.Body,
			Position:          e.Section.Position,
			SegmentConfidence: e.Section.Confidence,
		}
		if f := e.Finding; f != nil {
			ef := &oscal.ExportFinding{
				Relevant:    f.Relevant,
				Grounded:    f.Grounded,
				Requirement: f.Requirement,
				Commentary:  f.Commentary,
				Gaps:        f.Gaps,
				Quote:       f.Quote,
				Confidence:  f.Confidence,
				Model:       f.Model,
				PromptHash:  f.PromptHash,
				Revision:    f.Revision,
			}
			for _, m := range f.Mappings {
				ef.Mappings = append(ef.Mappings, oscal.ExportRegMapping{
					Kind: m.Kind, Ref: m.Ref, Rationale: m.Rationale, Confidence: m.Confidence, Source: m.Source, Known: m.Known,
				})
			}
			s.Finding = ef
		}
		out.Sections = append(out.Sections, s)
	}
	return out
}
