package assuranceplan

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Tests(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	quarter := 0
	if q := c.Query("quarter"); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 4 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "quarter must be 1 to 4"})
			return
		}
		quarter = n
	}
	tests, err := h.service.Tests(id, quarter)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, tests)
}

func (h *Handler) Deviations(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	d, err := h.service.Deviations(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) Dashboard(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	d, err := h.service.Dashboard(id)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, d)
}

func (h *Handler) ReplaceAttributes(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var attrs []Attribute
	if err := c.ShouldBindJSON(&attrs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the list of test attributes"})
		return
	}
	p, err := h.service.ReplaceAttributes(id, attrs, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, publicPlan(p))
}

func (h *Handler) SeedAttributes(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in SeedInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": `invalid JSON body: expected {"nfr_keys": [...], "control_ids": [...]}`})
		return
	}
	res, err := h.service.SeedAttributes(id, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	res.Plan = publicPlan(res.Plan)
	c.JSON(http.StatusOK, res)
}

func (h *Handler) CreateTests(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in CreateTestsInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the quarter and, optionally, the selections"})
		return
	}
	tests, err := h.service.CreateTests(id, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, tests)
}

func (h *Handler) SaveTest(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	testID, ok := parseID(c, "testID")
	if !ok {
		return
	}
	var in TestInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the test and its results"})
		return
	}
	res, err := h.service.SaveTest(id, testID, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) SignOffQA(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	testID, ok := parseID(c, "testID")
	if !ok {
		return
	}
	res, err := h.service.SignOffQA(id, testID, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (h *Handler) RaiseFinding(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	resultID, ok := parseID(c, "resultID")
	if !ok {
		return
	}
	var in RaiseInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the severity and owner"})
		return
	}
	res, err := h.service.RaiseFinding(id, resultID, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}

func (h *Handler) PushThematic(c *gin.Context) {
	id, ok := parseID(c, "id")
	if !ok {
		return
	}
	var in ThematicInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body: expected the attribute, stratum, owner, likelihood and impact"})
		return
	}
	res, err := h.service.PushThematic(id, in, h.actorOf(c))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, res)
}
