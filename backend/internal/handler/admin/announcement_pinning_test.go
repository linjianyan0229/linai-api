package admin

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type announcementPinCapture struct {
	service.AnnouncementRepository
	calls  int
	actor  int64
	pinned bool
}

func (r *announcementPinCapture) SetPinned(_ context.Context, id int64, pinned bool, actor *int64) (*service.Announcement, error) {
	r.calls++
	r.actor, r.pinned = *actor, pinned
	return &service.Announcement{ID: id, IsPinned: pinned}, nil
}

func TestAnnouncementPinRequiresExplicitStateAndIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name, id, body string
		authenticated  bool
		status         int
		pinned         bool
	}{
		{"pin", "4", `{"is_pinned":true}`, true, 200, true},
		{"unpin", "4", `{"is_pinned":false}`, true, 200, false},
		{"missing", "4", `{}`, true, 400, false},
		{"null", "4", `{"is_pinned":null}`, true, 400, false},
		{"wrong type", "4", `{"is_pinned":"true"}`, true, 400, false},
		{"invalid ID", "-1", `{"is_pinned":true}`, true, 400, false},
		{"unauthenticated", "4", `{"is_pinned":true}`, false, 401, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &announcementPinCapture{}
			h := NewAnnouncementHandler(service.NewAnnouncementService(repo, nil, nil, nil))
			router := gin.New()
			router.PUT("/announcements/:id/pin", func(c *gin.Context) {
				if test.authenticated {
					c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 77})
				}
				h.SetPinned(c)
			})
			req := httptest.NewRequest("PUT", "/announcements/"+test.id+"/pin", strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, test.status, response.Code, response.Body.String())
			if test.status != 200 {
				require.Zero(t, repo.calls)
				return
			}
			require.Equal(t, int64(77), repo.actor)
			require.Equal(t, test.pinned, repo.pinned)
			var body struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			require.Equal(t, test.pinned, body.Data["is_pinned"])
		})
	}
}
