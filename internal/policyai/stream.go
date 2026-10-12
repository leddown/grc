package policyai

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// Streaming: the answer's text is shown while the model writes it, and the
// validated result follows as it always has. What streams is a preview of
// answer_markdown only; edits are never shown before validation, and the
// preview is replaced by the recorded answer when the result arrives.

// Stream receives the stream's events. Answer carries the next piece of the
// answer's text; Restart says the text so far is void, because the answer is
// being asked for again (the one repair turn).
type Stream interface {
	Answer(text string)
	Restart()
}

// answerPrefix is how an answer in the contract begins: structured outputs
// write properties in schema order, so answer_markdown comes first. An answer
// that begins any other way is not previewed; its result still arrives.
var answerPrefix = regexp.MustCompile(`^\s*\{\s*"answer_markdown"\s*:\s*"`)

// answerReader pulls answer_markdown's text out of a JSON answer as it
// arrives, decoding escapes only once they are complete.
type answerReader struct {
	raw     strings.Builder
	start   int // offset of the string's first byte in raw, once found
	decoded int // bytes of the string already handed on
	sent    int // bytes of text sent, bounded by maxAnswerMarkdown
	done    bool
	out     func(string)
}

func (a *answerReader) write(delta string) {
	if a.done {
		return
	}
	a.raw.WriteString(delta)
	s := a.raw.String()
	if a.start == 0 {
		m := answerPrefix.FindStringIndex(s)
		if m == nil {
			if len(s) > 64 || strings.TrimSpace(s) != "" && !strings.HasPrefix(strings.TrimSpace(s), "{") {
				a.done = true
			}
			return
		}
		a.start = m[1]
	}
	body := s[a.start+a.decoded:]
	cut, closed := completeJSONString(body)
	if cut > 0 {
		var text string
		if err := json.Unmarshal([]byte(`"`+body[:cut]+`"`), &text); err != nil {
			a.done = true
			return
		}
		a.decoded += cut
		if room := maxAnswerMarkdown - a.sent; len(text) > room {
			text, a.done = strings.ToValidUTF8(text[:max(room, 0)], ""), true
		}
		a.sent += len(text)
		if text != "" {
			a.out(text)
		}
	}
	if closed {
		a.done = true
	}
}

// completeJSONString reports how much of the body of a JSON string (after its
// opening quote) can be decoded now: up to its closing quote when that has
// arrived, otherwise up to any escape that is still incomplete. A high
// surrogate is held until its pair arrives.
func completeJSONString(body string) (cut int, closed bool) {
	for i := 0; i < len(body); {
		switch body[i] {
		case '"':
			return i, true
		case '\\':
			if i+1 >= len(body) {
				return i, false
			}
			if body[i+1] != 'u' {
				i += 2
				continue
			}
			if i+6 > len(body) {
				return i, false
			}
			if hex := strings.ToLower(body[i+2 : i+4]); hex >= "d8" && hex <= "db" {
				if i+12 > len(body) {
					return i, false
				}
				i += 12
				continue
			}
			i += 6
		default:
			i++
		}
	}
	// A raw character may be split across pieces; its bytes are held until
	// the rest of it arrives.
	cut = len(body)
	for k := cut - 1; k >= 0 && k >= cut-utf8.UTFMax; k-- {
		if utf8.RuneStart(body[k]) {
			if !utf8.FullRuneInString(body[k:]) {
				cut = k
			}
			break
		}
	}
	return cut, false
}

// SSE writes a stream of server-sent events. It starts the response only on
// the first event, so a request refused before the model is asked is answered
// with an ordinary status and JSON body.
type SSE struct {
	c       *gin.Context
	mu      sync.Mutex
	started bool
}

// WantsStream reports whether the client asked for server-sent events.
func WantsStream(c *gin.Context) bool {
	return strings.Contains(c.GetHeader("Accept"), "text/event-stream")
}

// NewSSE returns a writer for c's response.
func NewSSE(c *gin.Context) *SSE { return &SSE{c: c} }

// Event writes one event; v is sent as JSON on one data line.
func (s *SSE) Event(name string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		h := s.c.Writer.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Accel-Buffering", "no")
		s.c.Status(http.StatusOK)
		s.started = true
	}
	_, _ = s.c.Writer.WriteString("event: " + name + "\ndata: " + string(raw) + "\n\n")
	s.c.Writer.Flush()
}

// Started reports whether the response has begun as a stream.
func (s *SSE) Started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *SSE) Answer(text string) { s.Event("answer", gin.H{"text": text}) }

func (s *SSE) Restart() { s.Event("restart", gin.H{}) }

// Fail writes an engine error: as an error event once the stream has begun,
// otherwise as Fail does.
func (s *SSE) Fail(err error) {
	if !s.Started() {
		Fail(s.c, err)
		return
	}
	msg := "the AI request could not be completed; try again"
	var r Refusal
	if errors.As(err, &r) {
		msg = r.Msg
	}
	s.Event("error", gin.H{"error": msg})
}
