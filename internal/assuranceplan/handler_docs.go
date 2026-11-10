package assuranceplan

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Renderer typesets a payload with a registered document template and writes
// the response. It is internal/doctemplate's, passed in by the app so this
// package does not import it.
type Renderer func(c *gin.Context, templateID string, data []byte, baseName string)

func (h *Handler) WithRenderer(r Renderer) *Handler {
	h.render = r
	return h
}

func (h *Handler) registerDocumentRoutes(r gin.IRouter) {
	r.GET("/assurance-plans/:id/export.json", h.ExportJSON)
	r.GET("/assurance-plans/:id/export.xlsx", h.ExportXLSX)
	r.GET("/assurance-plans/:id/export/:file", h.ExportCSV)
	r.GET("/assurance-plans/:id/pack.pdf", h.Pack)
	r.GET("/assurance-plans/:id/report/annual.pdf", h.AnnualReportPDF)
	r.GET("/assurance-plans/:id/report/quarter/:q", h.QuarterReportPDF)
}

func (h *Handler) ExportJSON(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	e, err := h.service.Export(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileStem(e.Reference, id)+".json"))
	c.JSON(http.StatusOK, e)
}

// ExportXLSX serves the plan workbook.
func (h *Handler) ExportXLSX(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	name, data, err := h.service.ExportXLSX(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

// ExportCSV serves one table of the plan, named by the file segment:
// steps.csv, quarters.csv, population.csv, tests.csv or deviations.csv.
func (h *Handler) ExportCSV(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	table, isCSV := strings.CutSuffix(c.Param("file"), ".csv")
	if !isCSV {
		c.JSON(http.StatusNotFound, gin.H{"error": "export files are " + strings.Join(ExportTables, ".csv, ") + ".csv"})
		return
	}
	name, data, err := h.service.ExportCSV(id, table)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func (h *Handler) Pack(c *gin.Context) {
	h.document(c, PackTemplateID, "pack", func(id int64) (any, string, error) {
		p, err := h.service.PackData(id)
		return p, fileStem(p.Reference, id) + "-pack", err
	})
}

func (h *Handler) AnnualReportPDF(c *gin.Context) {
	h.document(c, ReportTemplateID, "annual", func(id int64) (any, string, error) {
		r, err := h.service.AnnualReport(id)
		return r, r.Reference, err
	})
}

// QuarterReportPDF takes the quarter as 1 to 4 or Q1 to Q4, with or without
// ".pdf": /report/quarter/2, /report/quarter/Q2.pdf.
func (h *Handler) QuarterReportPDF(c *gin.Context) {
	raw := strings.TrimSuffix(strings.ToUpper(c.Param("q")), ".PDF")
	q, err := strconv.Atoi(strings.TrimPrefix(raw, "Q"))
	if err != nil || q < 1 || q > 4 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quarter must be 1 to 4"})
		return
	}
	h.document(c, ReportTemplateID, "quarter", func(id int64) (any, string, error) {
		r, err := h.service.QuarterReport(id, q)
		return r, r.Reference, err
	})
}

// document renders a payload, or returns it as JSON with ?format=json so the
// data behind a PDF can be read without an engine.
func (h *Handler) document(c *gin.Context, templateID, kind string, build func(int64) (any, string, error)) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	payload, base, err := build(id)
	if err != nil {
		writeError(c, err)
		return
	}
	if c.Query("format") == "json" {
		c.JSON(http.StatusOK, payload)
		return
	}
	if h.render == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "document rendering is not configured; add ?format=json for the " + kind + " data"})
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("encode %s payload: %v", kind, err)})
		return
	}
	h.render(c, templateID, data, fileStem(base, id))
}

func fileStem(reference string, id int64) string {
	if strings.TrimSpace(reference) == "" {
		return "assurance-plan-" + strconv.FormatInt(id, 10)
	}
	return reference
}
