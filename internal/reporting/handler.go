package reporting

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler is the example HTTP surface for the reporting package. It is wired
// into the app router via RegisterRoutes and demonstrates the full pipeline:
// fixture data -> html/template -> Renderer -> streamed PDF.
type Handler struct {
	service *Service
	logger  *slog.Logger
}

// NewHandler builds the reporting HTTP handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// RegisterRoutes attaches the reporting endpoints. Kept self-contained so the
// module can be wired into the app with a single call.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/reports/risk-assessment.pdf", h.SampleRiskAssessmentPDF)
	r.GET("/reports/control-assessment.pdf", h.SampleControlAssessmentPDF)
}

// SampleRiskAssessmentPDF renders the example Risk Assessment Report from
// in-memory fixtures and streams it as a PDF.
//
// Query params:
//   - draft=false       drops the DRAFT watermark
//   - download=1        forces a download (Content-Disposition: attachment)
func (h *Handler) SampleRiskAssessmentPDF(c *gin.Context) {
	report := SampleRiskAssessment()
	opts := SampleReportOptions()
	if c.Query("draft") == "false" {
		opts.Watermark = ""
	}

	pdf, err := h.service.RenderRiskAssessment(c.Request.Context(), report, opts)
	if err != nil {
		h.logger.Error("reporting.handler.render_failed", slog.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate report"})
		return
	}

	h.streamPDF(c, pdf, "risk-assessment-sample.pdf")
}

// SampleControlAssessmentPDF renders the example Control Assessment Report from
// in-memory fixtures and streams it as a PDF. Supports the same query params as
// SampleRiskAssessmentPDF (draft, download).
func (h *Handler) SampleControlAssessmentPDF(c *gin.Context) {
	report := SampleControlAssessment()
	opts := SampleControlAssessmentOptions()
	if c.Query("draft") == "true" {
		opts.Watermark = "DRAFT"
	}

	pdf, err := h.service.RenderControlAssessment(c.Request.Context(), report, opts)
	if err != nil {
		h.logger.Error("reporting.handler.render_failed", slog.String("report", "control_assessment"), slog.String("error", err.Error()))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate report"})
		return
	}
	h.streamPDF(c, pdf, "control-assessment-sample.pdf")
}

// streamPDF writes a PDF response, honoring ?download=1 for attachment delivery.
func (h *Handler) streamPDF(c *gin.Context, pdf []byte, filename string) {
	disposition := "inline"
	if c.Query("download") == "1" {
		disposition = "attachment"
	}
	c.Header("Content-Disposition", disposition+`; filename="`+filename+`"`)
	c.Data(http.StatusOK, "application/pdf", pdf)
}
