package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"carelockconsulting/internal/db"
)

const storedJSONListLimit = 200

var activeStoredJSONDB *db.Conn

type storedJSONSaveRequest struct {
	Name    string          `json:"name"`
	Source  string          `json:"source"`
	Content json.RawMessage `json:"content"`
}

type storedJSONDocumentMetadata struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
}

func configureStoredJSONStore(conn *db.Conn) {
	activeStoredJSONDB = conn
}

func saveStoredJSONDocument(c *gin.Context) {
	store := activeStoredJSONDB
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stored JSON database is not configured"})
		return
	}

	var req storedJSONSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	contentJSON := strings.TrimSpace(string(req.Content))
	if contentJSON == "" || !json.Valid([]byte(contentJSON)) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content must be valid JSON"})
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "stored_json_" + time.Now().UTC().Format("20060102_150405") + ".json"
	}
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "manual"
	}
	createdAt := time.Now().UTC().Format(time.RFC3339)

	id, err := store.Insert(`
		INSERT INTO stored_json_documents (name, source, content_json, created_at)
		VALUES (?, ?, ?, ?)
	`, name, source, contentJSON, createdAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save JSON document"})
		return
	}

	c.JSON(http.StatusOK, storedJSONDocumentMetadata{
		ID:        id,
		Name:      name,
		Source:    source,
		CreatedAt: createdAt,
	})
}

func listStoredJSONDocuments(c *gin.Context) {
	store := activeStoredJSONDB
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stored JSON database is not configured"})
		return
	}

	rows, err := store.Query(`
		SELECT id, name, source, created_at
		FROM stored_json_documents
		ORDER BY id DESC
		LIMIT ?
	`, storedJSONListLimit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list JSON documents"})
		return
	}
	defer rows.Close()

	items := make([]storedJSONDocumentMetadata, 0)
	for rows.Next() {
		var item storedJSONDocumentMetadata
		if err := rows.Scan(&item.ID, &item.Name, &item.Source, &item.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read JSON document"})
			return
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read JSON documents"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"items": items})
}

func getStoredJSONDocument(c *gin.Context) {
	store := activeStoredJSONDB
	if store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stored JSON database is not configured"})
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid stored JSON id"})
		return
	}

	var item storedJSONDocumentMetadata
	var contentJSON string
	err = store.QueryRow(`
		SELECT id, name, source, created_at, content_json
		FROM stored_json_documents
		WHERE id = ?
	`, id).Scan(&item.ID, &item.Name, &item.Source, &item.CreatedAt, &contentJSON)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "stored JSON document not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load JSON document"})
		return
	}

	var content any
	if err := json.Unmarshal([]byte(contentJSON), &content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "stored JSON document is invalid"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"id":         item.ID,
		"name":       item.Name,
		"source":     item.Source,
		"created_at": item.CreatedAt,
		"content":    content,
	})
}
