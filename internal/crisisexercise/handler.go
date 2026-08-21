package crisisexercise

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// designTimeout bounds one generation run. A twelve-phase exercise is a model
// call per phase, so a full design is a long wait — but a bounded one, because
// the request is synchronous and a wedged run would otherwise hold the handler
// open and keep spending.
const designTimeout = 20 * time.Minute

// adviseTimeout bounds one persona turn or probe.
const adviseTimeout = 5 * time.Minute

// ActorFunc resolves the acting user from a request, returning "" when there is
// none (local mode). A function rather than a dependency on internal/authn,
// matching internal/regcoverage and internal/policydocs.
type ActorFunc func(c *gin.Context) string

type Handler struct {
	service *Service
	actor   ActorFunc
}

func NewHandler(service *Service, actor ActorFunc) *Handler {
	return &Handler{service: service, actor: actor}
}

// RegisterRoutes attaches the read surface: the pages, the exercise in its
// various forms, and the seeded catalogs.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/crisis-exercises", h.IndexPage)
	r.GET("/crisis-exercises/catalog", h.Catalog)
	r.GET("/crisis-exercises/exercises", h.ListExercises)
	r.GET("/crisis-exercises/suggest", h.SuggestReferences)
	r.GET("/crisis-exercises/:id", h.ExercisePage)
	r.GET("/crisis-exercises/:id/report.json", h.ReportJSON)
	r.GET("/crisis-exercises/:id/report.pdf", h.ReportPDF)
	r.GET("/crisis-exercises/:id/msel.csv", h.MSEL)
	r.GET("/crisis-exercises/:id/handout.md", h.Handout)
	r.GET("/crisis-exercises/:id/coverage", h.Coverage)
	r.GET("/crisis-exercises/:id/versions", h.ListVersions)
	r.GET("/crisis-exercises/:id/chat", h.ListChat)
}

// RegisterAdminRoutes attaches everything that writes or spends.
//
// The whole write surface is here, including recording what happened during
// delivery. That is a deliberate choice rather than an oversight: an exercise
// record is evidence a supervisor may read, and "anyone with the page open
// could edit the observations" is not a property it should have.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/crisis-exercises/exercises", h.Create)
	r.PATCH("/crisis-exercises/:id", h.Update)
	r.DELETE("/crisis-exercises/:id", h.Delete)
	r.POST("/crisis-exercises/:id/clone", h.Clone)

	r.POST("/crisis-exercises/:id/objectives", h.SaveObjective)
	r.POST("/crisis-exercises/:id/phases", h.SavePhase)
	r.POST("/crisis-exercises/:id/participants", h.SaveParticipants)

	r.POST("/crisis-exercises/:id/injects", h.SaveInject)
	r.DELETE("/crisis-exercises/:id/injects/:childID", h.DeleteInject)
	r.POST("/crisis-exercises/:id/injects/:childID/response", h.RecordResponse)
	r.POST("/crisis-exercises/:id/injects/:childID/probe", h.ProbeInject)

	r.POST("/crisis-exercises/:id/decisions", h.SaveDecision)
	r.DELETE("/crisis-exercises/:id/decisions/:childID", h.DeleteDecision)

	r.PUT("/crisis-exercises/:id/classification", h.SaveClassification)
	r.POST("/crisis-exercises/:id/clocks/:childID", h.RecordNotification)

	r.POST("/crisis-exercises/:id/findings", h.SaveFinding)
	r.DELETE("/crisis-exercises/:id/findings/:childID", h.DeleteFinding)

	r.POST("/crisis-exercises/:id/references", h.AddReference)
	r.DELETE("/crisis-exercises/:id/references/:childID", h.DeleteReference)

	r.POST("/crisis-exercises/:id/design", h.Design)
	r.POST("/crisis-exercises/:id/advise", h.Advise)
	r.POST("/crisis-exercises/:id/after-action", h.DraftAfterAction)
	r.POST("/crisis-exercises/:id/versions", h.CutVersion)
	r.DELETE("/crisis-exercises/:id/chat", h.ClearChat)
}

func (h *Handler) actorOf(c *gin.Context) string {
	if h.actor == nil {
		return ""
	}
	return h.actor(c)
}

// ---- pages ----

func (h *Handler) IndexPage(c *gin.Context) {
	exercises, err := h.service.ListExercises()
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8",
		[]byte(indexPageHTML(exercises, h.service.Configured(), h.service.Model())))
}

func (h *Handler) ExercisePage(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	d, err := h.service.Dossier(id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	versions, err := h.service.ListVersions(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	turns, err := h.service.ListChat(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(exercisePageHTML(exercisePageData{
		Dossier:      d,
		Versions:     versions,
		Chat:         turns,
		AIConfigured: h.service.Configured(),
		Model:        h.service.Model(),
		PDFAvailable: h.service.PDFAvailable(),
	})))
}

// ---- catalogs ----

// Catalog serves everything the design page needs to render its pickers, and
// everything an external caller needs to build an exercise without guessing at
// this module's vocabulary.
func (h *Handler) Catalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"formats":       Formats(),
		"audiences":     Audiences(),
		"entity_types":  EntityTypes(),
		"jurisdictions": Jurisdictions(),
		"scenarios":     ScenarioLibrary(),
		"phases":        DefaultPhases(),
		"roles":         Roles(),
		"personas":      Personas(),
		"authorities":   Authorities(),
		"ref_kinds":     ReferenceKinds(),
		"configured":    h.service.Configured(),
		"model":         h.service.Model(),
	})
}

func (h *Handler) ListExercises(c *gin.Context) {
	exercises, err := h.service.ListExercises()
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"exercises":  exercises,
		"configured": h.service.Configured(),
		"model":      h.service.Model(),
	})
}

func (h *Handler) SuggestReferences(c *gin.Context) {
	query := strings.TrimSpace(c.Query("q"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a query is required"})
		return
	}
	limit, err := strconv.Atoi(strings.TrimSpace(c.Query("limit")))
	if err != nil || limit <= 0 || limit > 20 {
		limit = 5
	}
	c.JSON(http.StatusOK, gin.H{"candidates": h.service.SuggestReferences(query, limit)})
}

// ---- JSON reads ----

func (h *Handler) ReportJSON(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	d, err := h.service.Dossier(id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) Coverage(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	rows, err := h.service.Coverage(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"coverage": rows})
}

func (h *Handler) ListVersions(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	versions, err := h.service.ListVersions(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"versions": versions})
}

func (h *Handler) ListChat(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	turns, err := h.service.ListChat(id)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"turns":      turns,
		"personas":   Personas(),
		"configured": h.service.Configured(),
	})
}

// ---- documents ----

func (h *Handler) ReportPDF(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(c, pdfTimeout)
	defer cancel()

	pdf, filename, err := h.service.RenderPDF(ctx, id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Content-Disposition", contentDisposition("attachment", filename))
	c.Data(http.StatusOK, "application/pdf", pdf)
}

// MSEL serves the controller's copy of the master scenario events list.
//
// It carries the expected actions and the evaluation notes, which is the
// document a player must not see, so it is marked with the exercise's TLP in
// the first column of every row and served as a download rather than rendered
// in a page someone might have open on a shared screen.
func (h *Handler) MSEL(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	d, err := h.service.Dossier(id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	body, err := MSELCSV(d)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Content-Disposition", contentDisposition("attachment",
		fmt.Sprintf("%s-msel-controller-copy.csv", d.Exercise.Reference)))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", body)
}

// Handout serves the player brief: the same exercise with the answers removed.
func (h *Handler) Handout(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	d, err := h.service.Dossier(id, versionParam(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Content-Disposition", contentDisposition("attachment",
		fmt.Sprintf("%s-player-handout.md", d.Exercise.Reference)))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(PlayerHandoutMarkdown(d)))
}

// ---- mutations ----

type createRequest struct {
	Reference       string   `json:"reference"`
	Title           string   `json:"title"`
	Summary         string   `json:"summary"`
	Format          string   `json:"format"`
	Audience        string   `json:"audience"`
	EntityName      string   `json:"entity_name"`
	EntityType      string   `json:"entity_type"`
	Jurisdiction    string   `json:"jurisdiction"`
	Supervision     string   `json:"supervision"`
	CriticalFuncs   string   `json:"critical_functions"`
	ScenarioKey     string   `json:"scenario_key"`
	ThreatActor     string   `json:"threat_actor"`
	ThreatNarrative string   `json:"threat_narrative"`
	InitialVector   string   `json:"initial_vector"`
	TLP             string   `json:"tlp"`
	ScheduledFor    string   `json:"scheduled_for"`
	DurationMinutes int      `json:"duration_minutes"`
	Facilitator     string   `json:"facilitator"`
	ControlTeam     string   `json:"control_team"`
	Evaluators      string   `json:"evaluators"`
	PhaseKeys       []string `json:"phase_keys"`
}

func (h *Handler) Create(c *gin.Context) {
	var req createRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	ex, err := h.service.Create(CreateInput{
		Reference:       req.Reference,
		Title:           req.Title,
		Summary:         req.Summary,
		Format:          req.Format,
		Audience:        req.Audience,
		EntityName:      req.EntityName,
		EntityType:      req.EntityType,
		Jurisdiction:    req.Jurisdiction,
		Supervision:     req.Supervision,
		CriticalFuncs:   req.CriticalFuncs,
		ScenarioKey:     req.ScenarioKey,
		ThreatActor:     req.ThreatActor,
		ThreatNarrative: req.ThreatNarrative,
		InitialVector:   req.InitialVector,
		TLP:             req.TLP,
		ScheduledFor:    req.ScheduledFor,
		DurationMinutes: req.DurationMinutes,
		Facilitator:     req.Facilitator,
		ControlTeam:     req.ControlTeam,
		Evaluators:      req.Evaluators,
		PhaseKeys:       req.PhaseKeys,
		CreatedBy:       h.actorOf(c),
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, ex)
}

func (h *Handler) Update(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var in UpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	in.Actor = h.actorOf(c)
	ex, err := h.service.Update(id, in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, ex)
}

func (h *Handler) Delete(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := h.service.DeleteExercise(id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) Clone(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	_ = c.ShouldBindJSON(&req)

	ex, err := h.service.Clone(id, req.Title, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, ex)
}

func (h *Handler) SaveObjective(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var o Objective
	if err := c.ShouldBindJSON(&o); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	saved, err := h.service.SaveObjective(id, o)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) SavePhase(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var p Phase
	if err := c.ShouldBindJSON(&p); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	saved, err := h.service.SavePhase(id, p)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) SaveParticipants(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Participants []Participant `json:"participants"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	saved, err := h.service.SaveParticipants(id, req.Participants)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"participants": saved})
}

func (h *Handler) SaveInject(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var in Inject
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	saved, err := h.service.SaveInject(id, in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) DeleteInject(c *gin.Context) {
	injectID, ok := childParam(c, "inject")
	if !ok {
		return
	}
	if err := h.service.DeleteInject(injectID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) RecordResponse(c *gin.Context) {
	injectID, ok := childParam(c, "inject")
	if !ok {
		return
	}
	var resp Response
	if err := c.ShouldBindJSON(&resp); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(resp.Evaluator) == "" {
		resp.Evaluator = h.actorOf(c)
	}
	saved, err := h.service.RecordResponse(injectID, resp)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) SaveDecision(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var d Decision
	if err := c.ShouldBindJSON(&d); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	saved, err := h.service.SaveDecision(id, d)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) DeleteDecision(c *gin.Context) {
	decisionID, ok := childParam(c, "decision")
	if !ok {
		return
	}
	if err := h.service.DeleteDecision(decisionID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) SaveClassification(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var cls Classification
	if err := c.ShouldBindJSON(&cls); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	saved, clocks, err := h.service.SaveClassification(id, cls, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"classification": saved, "clocks": clocks})
}

func (h *Handler) RecordNotification(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	clockID, ok := childParam(c, "clock")
	if !ok {
		return
	}
	var req struct {
		ActualOffset int    `json:"actual_offset"`
		Evidence     string `json:"evidence"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	clock, err := h.service.RecordNotification(id, clockID, req.ActualOffset, req.Evidence)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, clock)
}

func (h *Handler) SaveFinding(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var f Finding
	if err := c.ShouldBindJSON(&f); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if strings.TrimSpace(f.CreatedBy) == "" {
		f.CreatedBy = h.actorOf(c)
	}
	saved, err := h.service.SaveFinding(id, f)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, saved)
}

func (h *Handler) DeleteFinding(c *gin.Context) {
	findingID, ok := childParam(c, "finding")
	if !ok {
		return
	}
	if err := h.service.DeleteFinding(findingID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) AddReference(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var ref Reference
	if err := c.ShouldBindJSON(&ref); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	saved, err := h.service.AddReference(id, ref)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, saved)
}

func (h *Handler) DeleteReference(c *gin.Context) {
	refID, ok := childParam(c, "reference")
	if !ok {
		return
	}
	if err := h.service.DeleteReference(refID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- AI ----

func (h *Handler) Design(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var in DesignInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	in.Actor = h.actorOf(c)

	ctx, cancel := contextWithTimeout(c, designTimeout)
	defer cancel()

	result, err := h.service.Design(ctx, id, in)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) Advise(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Persona  string `json:"persona"`
		Scope    string `json:"scope"`
		Question string `json:"question"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	ctx, cancel := contextWithTimeout(c, adviseTimeout)
	defer cancel()

	turn, err := h.service.Advise(ctx, id, req.Persona, req.Scope, req.Question, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, turn)
}

func (h *Handler) ProbeInject(c *gin.Context) {
	injectID, ok := childParam(c, "inject")
	if !ok {
		return
	}
	var req struct {
		Persona string `json:"persona"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	ctx, cancel := contextWithTimeout(c, adviseTimeout)
	defer cancel()

	turn, err := h.service.ProbeInject(ctx, injectID, req.Persona, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, turn)
}

func (h *Handler) DraftAfterAction(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	ctx, cancel := contextWithTimeout(c, designTimeout)
	defer cancel()

	result, err := h.service.DraftAfterAction(ctx, id, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *Handler) CutVersion(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
		Note string `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	v, err := h.service.CutVersion(id, req.Kind, req.Note, h.actorOf(c))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, v)
}

func (h *Handler) ClearChat(c *gin.Context) {
	id, ok := idParam(c)
	if !ok {
		return
	}
	if err := h.service.ClearChat(id); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---- helpers ----

func idParam(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid exercise id"})
		return 0, false
	}
	return id, true
}

// childParam reads the second path parameter. Gin requires one wildcard name
// per position across sibling routes, so every child route uses :childID and
// the handler names it in the error message.
func childParam(c *gin.Context, what string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("childID"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + what + " id"})
		return 0, false
	}
	return id, true
}

// versionParam reads ?version=N. Anything unparseable means the live record,
// which is the same thing an absent parameter means.
func versionParam(c *gin.Context) int {
	n, err := strconv.Atoi(strings.TrimSpace(c.Query("version")))
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// contextWithTimeout bounds the work and inherits the request's cancellation: a
// facilitator who closes the tab mid-generation should not leave the run
// spending.
func contextWithTimeout(c *gin.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), d)
}

// contentDisposition builds the header with a filename that cannot break out of
// the quoted string.
func contentDisposition(kind, filename string) string {
	safe := strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\\' || r == 127 {
			return -1
		}
		return r
	}, filename)
	if strings.TrimSpace(safe) == "" {
		safe = "exercise"
	}
	return fmt.Sprintf("%s; filename=%q", kind, safe)
}

// writeServiceError maps the module's error vocabulary onto status codes.
func writeServiceError(c *gin.Context, err error) {
	var notFoundErr ErrNotFound
	var invalidErr ErrInvalid
	switch {
	case errors.As(err, &notFoundErr):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.As(err, &invalidErr):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "the request timed out"})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
	}
}
