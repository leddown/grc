package apiutil

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// ErrUploadTooLarge is returned when an uploaded file is over the caller's
// limit.
var ErrUploadTooLarge = errors.New("upload is too large")

// ReadUpload reads a JSON file sent either as the "file" field of a multipart
// form or as the raw request body, refusing anything over limit bytes.
func ReadUpload(c *gin.Context, limit int64) ([]byte, error) {
	// Applied before FormFile so it bounds the multipart and the raw-body path.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)

	var reader io.Reader = c.Request.Body
	if file, _, err := c.Request.FormFile("file"); err == nil {
		defer file.Close()
		reader = file
	} else if !errors.Is(err, http.ErrNotMultipart) && !errors.Is(err, http.ErrMissingFile) {
		return nil, uploadError(err)
	}

	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, uploadError(err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, errors.New("no file was sent")
	}
	return raw, nil
}

// BoolParam reads a true/false flag from the query string or a form field.
func BoolParam(c *gin.Context, name string) bool {
	value, ok := c.GetQuery(name)
	if !ok {
		value = c.PostForm(name)
	}
	parsed, _ := strconv.ParseBool(strings.TrimSpace(value))
	return parsed
}

func uploadError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return fmt.Errorf("%w (limit %d bytes)", ErrUploadTooLarge, tooLarge.Limit)
	}
	return fmt.Errorf("read upload: %w", err)
}
