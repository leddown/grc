package controlcatalog

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"grc/internal/apiutil"
)

// SetOSCALReader installs the reader the OSCAL import uses; without one the
// import routes answer 503.
func (h *Handler) SetOSCALReader(reader OSCALReader) { h.oscalReader = reader }

// oscalImportRequest is the one JSON body the Control Editor posts: the text of
// each selected file, so the SHA-256 the result reports is the hash of the file
// itself, and the preview's base on apply.
type oscalImportRequest struct {
	Catalog  string            `json:"catalog"`
	Profiles map[string]string `json:"profiles"`
	Base     string            `json:"base"`
}

func (h *Handler) readOSCALImport(c *gin.Context) (OSCALCatalog, oscalImportRequest, bool) {
	var req oscalImportRequest
	if h.oscalReader == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "the OSCAL import is not available"})
		return OSCALCatalog{}, req, false
	}
	raw, err := apiutil.ReadUpload(c, maxImportBytes)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, apiutil.ErrUploadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return OSCALCatalog{}, req, false
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the body must be JSON: {\"catalog\": \"<file text>\", \"profiles\": {\"low\": \"<file text>\", ...}}"})
		return OSCALCatalog{}, req, false
	}
	if strings.TrimSpace(req.Catalog) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "choose the OSCAL catalog file"})
		return OSCALCatalog{}, req, false
	}
	profiles := map[string][]byte{}
	for slot, text := range req.Profiles {
		if strings.TrimSpace(text) != "" {
			profiles[strings.ToLower(strings.TrimSpace(slot))] = []byte(text)
		}
	}
	catalog, err := h.oscalReader([]byte(req.Catalog), profiles)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "the catalog file cannot be read: " + err.Error()})
		return OSCALCatalog{}, req, false
	}
	return catalog, req, true
}

// PreviewOSCALImport says what importing the posted OSCAL catalog would do:
// every problem, then the added, changed, unchanged and absent controls.
// Nothing is written.
func (h *Handler) PreviewOSCALImport(c *gin.Context) {
	catalog, _, ok := h.readOSCALImport(c)
	if !ok {
		return
	}
	result, err := h.service.PreviewOSCAL(catalog)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to preview the OSCAL import"})
		return
	}
	c.JSON(http.StatusOK, result)
}

// ImportOSCAL applies the posted OSCAL catalog against the preview named by
// base, then relinks the Security NFRs to the controls as the JSON import does.
func (h *Handler) ImportOSCAL(c *gin.Context) {
	catalog, req, ok := h.readOSCALImport(c)
	if !ok {
		return
	}
	result, err := h.service.ApplyOSCAL(catalog, req.Base)
	switch {
	case errors.Is(err, ErrOSCALProblems):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "result": result})
		return
	case errors.Is(err, ErrOSCALConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "result": result})
		return
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to apply the OSCAL import"})
		return
	}
	if h.linkBuilder != nil {
		if err := h.linkBuilder.Rebuild(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "imported, but failed to rebuild NFR-control links"})
			return
		}
		result.LinksRebuilt = true
	}
	c.JSON(http.StatusOK, result)
}
