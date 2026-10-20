package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/drywaters/dejaview/internal/session"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func bodyRequest(body, contentType string, unknownLength bool) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/?api_key=secret", strings.NewReader(body))
	r.Header.Set("Content-Type", contentType)
	if unknownLength {
		r.ContentLength = -1
		r.TransferEncoding = []string{"chunked"}
	}
	route := chi.NewRouteContext()
	route.URLParams.Add("id", uuid.NewString())
	route.URLParams.Add("num", "1")
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
}

func TestFormBodyLimit(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		for _, size := range []int{maxRequestBodyBytes, maxRequestBodyBytes + 1} {
			t.Run(fmt.Sprintf("size=%d/unknown=%t", size, unknown), func(t *testing.T) {
				r := bodyRequest("value="+strings.Repeat("a", size-len("value=")), "application/x-www-form-urlencoded", unknown)
				w := httptest.NewRecorder()
				err := parseLimitedForm(w, r)
				if size == maxRequestBodyBytes {
					if err != nil || len(r.PostForm.Get("value")) != size-len("value=") {
						t.Fatalf("boundary form was not parsed: %v", err)
					}
				} else if !requestBodyTooLarge(w, err) || w.Code != http.StatusRequestEntityTooLarge {
					t.Fatalf("oversized form error=%v status=%d", err, w.Code)
				}
			})
		}
	}
}

func TestFormHandlersRejectOversizedBodiesBeforeSideEffects(t *testing.T) {
	// Nil repositories make any downstream read or write fail this test.
	auth := NewAuthHandler("secret", session.NewManager("secret", time.Hour, false, 0), testLimiter(), nil)
	handlers := map[string]http.HandlerFunc{
		"login":         auth.Login,
		"entry":         (&EntryHandler{}).Update,
		"movie":         (&MovieHandler{}).AddFromTMDB,
		"logout":        auth.Logout,
		"delete entry":  (&EntryHandler{}).Delete,
		"delete person": (&PeopleHandler{}).Delete,
		"create person": (&PeopleHandler{}).Create,
		"update person": (&PeopleHandler{}).Update,
		"ratings":       (&RatingHandler{}).SaveRatings,
	}
	for name, handler := range handlers {
		for _, unknown := range []bool{false, true} {
			for _, contentType := range []string{"application/x-www-form-urlencoded", "text/plain", "multipart/form-data; boundary=body-limit"} {
				t.Run(fmt.Sprintf("%s/unknown=%t/%s", name, unknown, contentType), func(t *testing.T) {
					body := "api_key=secret&padding=" + strings.Repeat("x", maxRequestBodyBytes)
					w := httptest.NewRecorder()
					r := bodyRequest(body, contentType, unknown)
					if name == "delete entry" {
						r.Method = http.MethodDelete
					}
					handler(w, r)
					if w.Code != http.StatusRequestEntityTooLarge {
						t.Fatalf("status=%d body=%q", w.Code, w.Body.String())
					}
					if w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" || w.Header().Get("HX-Trigger") != "" {
						t.Fatalf("oversized request produced success/redirect headers: %v", w.Header())
					}
				})
			}
		}
	}
}

func TestMalformedFormsKeepExistingResponses(t *testing.T) {
	auth := NewAuthHandler("secret", session.NewManager("secret", time.Hour, false, 0), testLimiter(), nil)
	for _, tc := range []struct {
		name     string
		handler  http.HandlerFunc
		status   int
		location string
	}{
		{"login", auth.Login, http.StatusSeeOther, "/login?error=invalid_request"},
		{"entry", (&EntryHandler{}).Update, http.StatusBadRequest, ""},
		{"movie", (&MovieHandler{}).AddFromTMDB, http.StatusBadRequest, ""},
		{"create person", (&PeopleHandler{}).Create, http.StatusSeeOther, "/people?error=invalid_request"},
		{"update person", (&PeopleHandler{}).Update, http.StatusSeeOther, "/people?error=invalid_request"},
		{"ratings", (&RatingHandler{}).SaveRatings, http.StatusBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler(w, bodyRequest("value=%zz", "application/x-www-form-urlencoded", false))
			if w.Code != tc.status || w.Header().Get("Location") != tc.location {
				t.Fatalf("status=%d location=%q", w.Code, w.Header().Get("Location"))
			}
		})
	}
}

func TestReorderReadsCompleteBoundedJSONBeforeWriting(t *testing.T) {
	const prefix = `{"entry_ids":[]}`
	for _, tc := range []struct {
		name, body string
		status     int
		writes     int
	}{
		{"at limit", prefix + strings.Repeat(" ", maxRequestBodyBytes-len(prefix)), http.StatusOK, 1},
		{"limit plus one", prefix + strings.Repeat(" ", maxRequestBodyBytes-len(prefix)+1), http.StatusRequestEntityTooLarge, 0},
		{"oversized first value", `{"entry_ids":["` + strings.Repeat("x", maxRequestBodyBytes) + `"]}`, http.StatusRequestEntityTooLarge, 0},
		{"second JSON value", prefix + `{}`, http.StatusBadRequest, 0},
		{"trailing garbage", prefix + `x`, http.StatusBadRequest, 0},
		{"malformed", `{`, http.StatusBadRequest, 0},
		{"whitespace", prefix + "\n\t ", http.StatusOK, 1},
	} {
		for _, unknown := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/unknown=%t", tc.name, unknown), func(t *testing.T) {
				store := &stubEntryStore{}
				h := &EntryHandler{entryRepo: store}
				w := httptest.NewRecorder()
				h.Reorder(w, bodyRequest(tc.body, "application/json", unknown))
				if w.Code != tc.status || store.reorderCalls != tc.writes {
					t.Fatalf("status=%d writes=%d; want status=%d writes=%d", w.Code, store.reorderCalls, tc.status, tc.writes)
				}
			})
		}
	}
}

func TestLogoutKeepsBoundedRedirectsAndCookieClear(t *testing.T) {
	h := NewAuthHandler("secret", session.NewManager("secret", time.Hour, false, 0), testLimiter(), nil)
	for _, tc := range []struct{ name, body, contentType, target string }{
		{"empty", "", "", "/login"},
		{"urlencoded", "redirect=%2Fpeople", "application/x-www-form-urlencoded", "/login?redirect=%2Fpeople"},
		{"multipart", "--normal\r\nContent-Disposition: form-data; name=\"redirect\"\r\n\r\n/people\r\n--normal--\r\n", "multipart/form-data; boundary=normal", "/login?redirect=%2Fpeople"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.Logout(w, bodyRequest(tc.body, tc.contentType, false))
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != tc.target {
				t.Fatalf("status=%d location=%q", w.Code, w.Header().Get("Location"))
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 || cookies[0].Name != session.CookieName || cookies[0].MaxAge >= 0 {
				t.Fatalf("logout did not clear the session cookie: %v", cookies)
			}
		})
	}
}

func TestOversizedMultipartLogoutDoesNotClearCookie(t *testing.T) {
	h := NewAuthHandler("secret", session.NewManager("secret", time.Hour, false, 0), testLimiter(), nil)
	for _, unknown := range []bool{false, true} {
		body := "--upload\r\nContent-Disposition: form-data; name=\"redirect\"\r\n\r\n/people\r\n--upload\r\nContent-Disposition: form-data; name=\"file\"; filename=\"large.txt\"\r\nContent-Type: text/plain\r\n\r\n" + strings.Repeat("x", maxRequestBodyBytes+1) + "\r\n--upload--\r\n"
		r := bodyRequest(body, "multipart/form-data; boundary=upload", unknown)
		w := httptest.NewRecorder()
		h.Logout(w, r)
		if w.Code != http.StatusRequestEntityTooLarge || w.Header().Get("Set-Cookie") != "" || w.Header().Get("Location") != "" {
			t.Fatalf("unknown=%t status=%d headers=%v", unknown, w.Code, w.Header())
		}
		if r.MultipartForm != nil {
			t.Fatal("oversized multipart upload was accepted")
		}
	}
}

func TestDeleteBodiesAreBoundedBeforeRepositoryMutation(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		people := &stubPeopleStore{}
		entries := &stubEntryStore{}
		for name, handler := range map[string]http.HandlerFunc{"person": (&PeopleHandler{personRepo: people}).Delete, "entry": (&EntryHandler{entryRepo: entries}).Delete} {
			r := bodyRequest(strings.Repeat("x", maxRequestBodyBytes+1), "application/x-www-form-urlencoded", unknown)
			if name == "entry" {
				r.Method = http.MethodDelete
			}
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != http.StatusRequestEntityTooLarge || people.deleteCalls != 0 || entries.deleteCalls != 0 {
				t.Fatalf("%s unknown=%t status=%d person deletes=%d entry deletes=%d", name, unknown, w.Code, people.deleteCalls, entries.deleteCalls)
			}
		}
	}
}
