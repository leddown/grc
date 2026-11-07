package assuranceplan

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	service *Service
	actor   func(*gin.Context) string
	render  Renderer
}

func NewHandler(service *Service, actor func(*gin.Context) string) *Handler {
	return &Handler{service: service, actor: actor}
}

// RegisterRoutes is the read surface, open to anyone with access to the page.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/assurance-plans", h.ListPage)
	r.GET("/assurance-plans/data", h.List)
	r.GET("/assurance-plans/data/:id", h.GetOne)
	r.GET("/assurance-plans/types", h.Types)
	r.GET("/assurance-plans/vocabulary", h.Vocabulary)
	r.GET("/assurance-plans/:id", h.PlanPage)
	r.GET("/assurance-plans/:id/lint", h.Lint)
	r.GET("/assurance-plans/:id/history", h.History)
	r.GET("/assurance-plans/:id/versions", h.Versions)
	r.GET("/assurance-plans/:id/versions/:n", h.Version)
	r.GET("/assurance-plans/:id/sampling", h.Sampling)
	r.GET("/assurance-plans/:id/selections", h.Selections)
	r.GET("/assurance-plans/:id/population", h.Population)
	r.GET("/assurance-plans/:id/population.csv", h.PopulationCSV)
	r.GET("/assurance-plans/:id/population/example.csv", h.ExamplePopulationCSV)
	r.GET("/assurance-plans/:id/tests", h.Tests)
	r.GET("/assurance-plans/:id/deviations", h.Deviations)
	r.GET("/assurance-plans/:id/dashboard", h.Dashboard)
	h.registerDocumentRoutes(r)
}

// RegisterAdminRoutes carries every write, approval included. An approved plan
// is what a supervisor reads as the control function's commitment, so changing
// it is not left to anyone who can read it.
func (h *Handler) RegisterAdminRoutes(r gin.IRouter) {
	r.POST("/assurance-plans", h.Create)
	r.PUT("/assurance-plans/:id", h.UpdateHeader)
	r.DELETE("/assurance-plans/:id", h.Delete)
	r.PUT("/assurance-plans/:id/steps", h.ReplaceSteps)
	r.PATCH("/assurance-plans/:id/steps/:stepID", h.UpdateStepStatus)
	r.PUT("/assurance-plans/:id/quarters", h.ReplaceQuarters)
	r.POST("/assurance-plans/:id/quarters/generate", h.GenerateQuarters)
	r.PUT("/assurance-plans/:id/governance", h.ReplaceGovernance)
	r.POST("/assurance-plans/:id/submit", h.Submit)
	r.POST("/assurance-plans/:id/return", h.Return)
	r.POST("/assurance-plans/:id/approve", h.Approve)
	r.POST("/assurance-plans/:id/activate", h.Activate)
	r.POST("/assurance-plans/:id/close", h.Close)
	r.POST("/assurance-plans/:id/change-request", h.ChangeRequest)
	r.POST("/assurance-plans/:id/clone", h.Clone)
	r.PUT("/assurance-plans/:id/strata", h.ReplaceStrata)
	r.POST("/assurance-plans/:id/population/propose", h.ProposePopulation)
	r.POST("/assurance-plans/:id/population/preview", h.PreviewPopulation)
	r.POST("/assurance-plans/:id/population", h.ApplyPopulation)
	r.PATCH("/assurance-plans/:id/population/:itemID", h.UpdatePopulationItem)
	r.POST("/assurance-plans/:id/population/freeze", h.FreezePopulation)
	r.POST("/assurance-plans/:id/quarters/:q/select", h.SelectQuarter)
	r.GET("/assurance-plans/:id/seed", h.Seed)
	r.PUT("/assurance-plans/:id/attributes", h.ReplaceAttributes)
	r.POST("/assurance-plans/:id/attributes/seed", h.SeedAttributes)
	r.POST("/assurance-plans/:id/tests", h.CreateTests)
	r.PUT("/assurance-plans/:id/tests/:testID", h.SaveTest)
	r.POST("/assurance-plans/:id/tests/:testID/qa", h.SignOffQA)
	r.POST("/assurance-plans/:id/results/:resultID/finding", h.RaiseFinding)
	r.POST("/assurance-plans/:id/thematic", h.PushThematic)
}

func (h *Handler) actorOf(c *gin.Context) string {
	if h.actor == nil {
		return ""
	}
	return h.actor(c)
}

func (h *Handler) filter(c *gin.Context) Filter {
	clientID, _ := strconv.ParseInt(c.Query("client"), 10, 64)
	return Filter{
		PlanType: strings.TrimSpace(c.Query("type")),
		Status:   strings.TrimSpace(c.Query("status")),
		Year:     strings.TrimSpace(c.Query("year")),
		ClientID: clientID,
		Search:   c.Query("q"),
	}
}

func (h *Handler) ListPage(c *gin.Context) {
	f := h.filter(c)
	plans, err := h.service.List(f)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to list assurance plans")
		return
	}
	clients, err := h.service.Clients()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to list clients")
		return
	}
	page := listPageHTML(listPageData{Plans: plans, Filter: f, Clients: clients, Templates: Templates()})
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

func (h *Handler) PlanPage(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	p, err := h.service.Get(id)
	if errors.Is(err, ErrNotFound) {
		c.String(http.StatusNotFound, "assurance plan not found")
		return
	}
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the assurance plan")
		return
	}
	history, err := h.service.History(id)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the plan's history")
		return
	}
	versions, err := h.service.Versions(id)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the plan's versions")
		return
	}
	clients, err := h.service.Clients()
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to list clients")
		return
	}
	pop, err := h.service.Population(id)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the plan's population")
		return
	}
	sampling, err := h.service.Sampling(id)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to size the plan's sample")
		return
	}
	tests, err := h.service.Tests(id, 0)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load the plan's tests")
		return
	}
	dashboard, err := h.service.Dashboard(id)
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to build the plan's dashboard")
		return
	}
	page := planPageHTML(planPageData{
		Plan: publicPlan(p), Lint: Lint(p, pop), History: history, Versions: versions, Clients: clients,
		Population: pop, Sampling: sampling, Tests: tests, Dashboard: dashboard,
		Today: time.Now().UTC().Format(dateLayout), Viewer: h.actorOf(c), SepEnforced: h.service.SeparationEnforced(),
	})
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(page))
}

func (h *Handler) List(c *gin.Context) {
	plans, err := h.service.List(h.filter(c))
	if err != nil {
		writeError(c, err)
		return
	}
	for i := range plans {
		plans[i] = publicPlan(plans[i])
	}
	c.JSON(http.StatusOK, plans)
}

func (h *Handler) GetOne(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	p, err := h.service.Get(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func (h *Handler) Types(c *gin.Context) { c.JSON(http.StatusOK, Templates()) }

func (h *Handler) Vocabulary(c *gin.Context) { c.JSON(http.StatusOK, Vocab()) }

func (h *Handler) Lint(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	findings, err := h.service.Lint(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"findings": findings, "blocking": len(BlockingFindings(findings))})
}

func (h *Handler) History(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.History(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) Versions(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.Versions(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

// Version serves the approved snapshot exactly as it was stored, so its
// SHA-256 can be checked against the bytes received.
func (h *Handler) Version(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	n, err := strconv.Atoi(c.Param("n"))
	if err != nil || n <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid version number"})
		return
	}
	v, err := h.service.Version(id, n)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("X-Snapshot-SHA256", v.SnapshotSHA256)
	c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(v.SnapshotJSON))
}

func (h *Handler) Create(c *gin.Context) {
	var in CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.Create(in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, publicPlan(p))
}

func (h *Handler) UpdateHeader(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in Plan
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.UpdateHeader(id, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func (h *Handler) Delete(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	if err := h.service.Delete(id); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *Handler) ReplaceSteps(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var steps []Step
	if err := c.ShouldBindJSON(&steps); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the list of steps"})
		return
	}
	p, err := h.service.ReplaceSteps(id, steps, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func (h *Handler) UpdateStepStatus(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	stepID, ok := parseID(c, "stepID")
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.UpdateStepStatus(id, stepID, in.Status, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func (h *Handler) ReplaceQuarters(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var quarters []Quarter
	if err := c.ShouldBindJSON(&quarters); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the list of quarters"})
		return
	}
	p, err := h.service.ReplaceQuarters(id, quarters, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func (h *Handler) GenerateQuarters(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in struct {
		Dates bool `json:"dates"`
	}
	_ = c.ShouldBindJSON(&in)
	p, err := h.service.GenerateQuarters(id, in.Dates, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func (h *Handler) ReplaceGovernance(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var g Governance
	if err := c.ShouldBindJSON(&g); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	p, err := h.service.ReplaceGovernance(id, g, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

type noteBody struct {
	Note       string `json:"note"`
	Reason     string `json:"reason"`
	Title      string `json:"title"`
	ApprovedBy string `json:"approved_by"`
}

func (h *Handler) lifecycle(c *gin.Context, run func(id int64, body noteBody, actor string) (any, error)) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body noteBody
	_ = c.ShouldBindJSON(&body)
	out, err := run(id, body, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	switch v := out.(type) {
	case Plan:
		out = publicPlan(v)
	case ApprovalResult:
		v.Plan = publicPlan(v.Plan)
		out = v
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) Submit(c *gin.Context) {
	h.lifecycle(c, func(id int64, _ noteBody, actor string) (any, error) { return h.service.Submit(id, actor) })
}

func (h *Handler) Return(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) { return h.service.Return(id, b.Note, actor) })
}

func (h *Handler) Activate(c *gin.Context) {
	h.lifecycle(c, func(id int64, _ noteBody, actor string) (any, error) { return h.service.Activate(id, actor) })
}

func (h *Handler) Close(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) { return h.service.Close(id, b.Note, actor) })
}

func (h *Handler) ChangeRequest(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) {
		return h.service.ChangeRequest(id, firstNonEmpty(b.Reason, b.Note), actor)
	})
}

// Approve records the signed-in user as the approver whenever there is one: a
// name typed into the request would let an author approve their own plan by
// typing someone else's. Only without a session (local mode) is the name taken
// from the body.
func (h *Handler) Approve(c *gin.Context) {
	h.lifecycle(c, func(id int64, b noteBody, actor string) (any, error) {
		approver := b.ApprovedBy
		if actor != "" {
			approver = actor
		}
		return h.service.Approve(id, approver, b.Note)
	})
}

func (h *Handler) Clone(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var body noteBody
	_ = c.ShouldBindJSON(&body)
	p, err := h.service.Clone(id, body.Title, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, publicPlan(p))
}

func (h *Handler) Sampling(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	v, err := h.service.Sampling(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

func (h *Handler) Selections(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.Selections(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) Population(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	items, err := h.service.Population(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, items)
}

func (h *Handler) PopulationCSV(c *gin.Context) {
	h.csv(c, "population", h.service.PopulationCSV)
}

func (h *Handler) ExamplePopulationCSV(c *gin.Context) {
	h.csv(c, "example-population", h.service.ExamplePopulationCSV)
}

func (h *Handler) csv(c *gin.Context, name string, build func(int64) (string, error)) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	text, err := build(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="assurance-plan-%d-%s.csv"`, id, name))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(text))
}

// maxPopulationBody caps a pasted population: 50,000 rows of a dozen short
// columns fit well inside it.
const maxPopulationBody = 16 << 20

func (h *Handler) populationImport(c *gin.Context) (int64, PopulationImport, bool) {
	id, ok := parseID(c, "id")
	if !ok {
		return 0, PopulationImport{}, false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPopulationBody)
	var in PopulationImport
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected {\"csv\": \"…\"} of at most 16 MiB"})
		return 0, PopulationImport{}, false
	}
	return id, in, true
}

// ProposePopulation answers the CSV text of a proposed population, for the
// preview: ?source=policies or ?source=findings, else the plan's own source.
// It writes nothing.
func (h *Handler) ProposePopulation(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	out, err := h.service.ProposePopulation(id, c.Query("source"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, out)
}

func (h *Handler) PreviewPopulation(c *gin.Context) {
	id, in, ok := h.populationImport(c)
	if !ok {
		return
	}
	diff, err := h.service.PreviewPopulation(id, in)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, diff)
}

func (h *Handler) ApplyPopulation(c *gin.Context) {
	id, in, ok := h.populationImport(c)
	if !ok {
		return
	}
	diff, err := h.service.ApplyPopulation(id, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, diff)
}

func (h *Handler) UpdatePopulationItem(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	itemID, ok := parseID(c, "itemID")
	if !ok {
		return
	}
	var patch PopulationPatch
	if err := c.ShouldBindJSON(&patch); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	item, err := h.service.UpdatePopulationItem(id, itemID, patch, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (h *Handler) FreezePopulation(c *gin.Context) {
	h.lifecycle(c, func(id int64, _ noteBody, actor string) (any, error) { return h.service.FreezePopulation(id, actor) })
}

func (h *Handler) SelectQuarter(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	q, err := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(c.Param("q")), "Q"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid quarter"})
		return
	}
	res, err := h.service.SelectQuarter(id, q, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	res.Plan = publicPlan(res.Plan)
	c.JSON(http.StatusOK, res)
}

// Seed is on the admin surface: readers see the commitment until the last
// quarter is drawn, so a later wave cannot be predicted from it.
func (h *Handler) Seed(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	seed, err := h.service.Seed(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"seed": seed, "commitment": seedCommitment(seed)})
}

func (h *Handler) ReplaceStrata(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var strata []Stratum
	if err := c.ShouldBindJSON(&strata); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the list of strata"})
		return
	}
	p, err := h.service.ReplaceStrata(id, strata, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func parseID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid " + name})
		return 0, false
	}
	return id, true
}

func writeError(c *gin.Context, err error) {
	var verr *ValidationError
	var cerr *ConflictError
	var aerr ApprovalError
	switch {
	case errors.As(err, &aerr):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": aerr.Error(), "findings": aerr.Findings})
	case errors.As(err, &verr):
		c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
	case errors.As(err, &cerr):
		c.JSON(http.StatusConflict, gin.H{"error": cerr.Error()})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "assurance plan storage failed"})
	}
}
