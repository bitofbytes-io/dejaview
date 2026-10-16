package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/drywaters/dejaview/internal/middleware"
	"github.com/drywaters/dejaview/internal/model"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/ui/pages"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const maxPersonNameLength = 50

// peopleErrors maps the ?error= codes the people page accepts to messages.
// Redirecting with a code, as login does, keeps rejected forms working with
// boosted navigation, which does not swap 4xx responses.
var peopleErrors = map[string]string{
	"invalid_name":    "Enter a name of up to 50 characters.",
	"invalid_initial": "The initial must be a single letter.",
	"invalid_color":   "Pick one of the listed colours.",
	"initial_taken":   "Someone already uses that initial. Choose a different one.",
	"not_found":       "That person no longer exists.",
	"in_use":          "Can't remove someone who has ratings or picks. Rename them instead.",
	"invalid_request": "Something went wrong with that form. Try again.",
}

// PeopleHandler manages the people who pick and rate movies.
type PeopleHandler struct {
	personRepo peopleStore
}

type peopleStore interface {
	GetAll(ctx context.Context) ([]*model.Person, error)
	Create(ctx context.Context, initial, name, color string) (*model.Person, error)
	Update(ctx context.Context, id uuid.UUID, name, color string) error
	Delete(ctx context.Context, id uuid.UUID) error
}

// NewPeopleHandler creates a new PeopleHandler
func NewPeopleHandler(personRepo *repository.PersonRepository) *PeopleHandler {
	return &PeopleHandler{personRepo: personRepo}
}

// PeoplePage lists everyone and the form to add a person.
func (h *PeopleHandler) PeoplePage(w http.ResponseWriter, r *http.Request) {
	people, err := h.personRepo.GetAll(r.Context())
	if err != nil {
		slog.Error("failed to list people", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	isAuthenticated := middleware.IsAuthenticated(r.Context())
	query := r.URL.Query()
	form := pages.PeopleFormData{Name: query.Get("name"), Initial: query.Get("initial"), Color: query.Get("color")}
	if !model.ValidPersonColor(form.Color) {
		form.Color = suggestColor(people)
	}
	errorMessage := peopleErrors[query.Get("error")]
	if err := pages.PeoplePage(people, form, errorMessage, isAuthenticated).Render(r.Context(), w); err != nil {
		slog.Error("failed to render people page", "error", err)
	}
}

// Create adds a person.
func (h *PeopleHandler) Create(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		redirectPeople(w, r, "invalid_request")
		return
	}
	// A rejected form comes back with what was typed.
	reject := func(code string) {
		query := url.Values{"error": {code}}
		for _, field := range []string{"name", "initial", "color"} {
			if value := r.PostFormValue(field); value != "" {
				query.Set(field, value)
			}
		}
		http.Redirect(w, r, "/people?"+query.Encode(), http.StatusSeeOther)
	}

	name, ok := cleanPersonName(r.PostFormValue("name"))
	if !ok {
		reject("invalid_name")
		return
	}

	initial := strings.TrimSpace(r.PostFormValue("initial"))
	if initial == "" {
		initial = string([]rune(name)[:1])
	}
	initial, ok = cleanPersonInitial(initial)
	if !ok {
		reject("invalid_initial")
		return
	}

	color := r.PostFormValue("color")
	if color == "" {
		people, err := h.personRepo.GetAll(r.Context())
		if err != nil {
			slog.Error("failed to list people", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		color = suggestColor(people)
	}
	if !model.ValidPersonColor(color) {
		reject("invalid_color")
		return
	}

	person, err := h.personRepo.Create(r.Context(), initial, name, color)
	if errors.Is(err, repository.ErrPersonInitialTaken) {
		reject("initial_taken")
		return
	}
	if err != nil {
		slog.Error("failed to create person", "error", err)
		http.Error(w, "Failed to add person", http.StatusInternalServerError)
		return
	}

	slog.Info("person created", "person_id", person.ID)
	redirectPeople(w, r, "")
}

// Update renames a person and sets their colour. A save from the person's
// card through htmx gets just that card back, so the page keeps its scroll
// position; a plain form post gets a redirect to the page.
func (h *PeopleHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid person ID", http.StatusBadRequest)
		return
	}
	inPlace := r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true"
	finish := func(code string) {
		if inPlace {
			h.renderPersonCard(w, r, id, code)
			return
		}
		redirectPeople(w, r, code)
	}
	if err := r.ParseForm(); err != nil {
		finish("invalid_request")
		return
	}

	name, ok := cleanPersonName(r.PostFormValue("name"))
	if !ok {
		finish("invalid_name")
		return
	}
	color := r.PostFormValue("color")
	if !model.ValidPersonColor(color) {
		finish("invalid_color")
		return
	}

	err = h.personRepo.Update(r.Context(), id, name, color)
	if errors.Is(err, repository.ErrPersonNotFound) {
		finish("not_found")
		return
	}
	if err != nil {
		slog.Error("failed to update person", "error", err)
		http.Error(w, "Failed to update person", http.StatusInternalServerError)
		return
	}

	slog.Info("person updated", "person_id", id)
	finish("")
}

// renderPersonCard answers an in-place save with the person's card. A
// rejected save keeps what was typed and shows why, with a 200 status because
// htmx does not swap error responses. A successful save also raises the
// "Saved!" toast. A person who no longer exists sends the browser back to the
// page with the not_found message.
func (h *PeopleHandler) renderPersonCard(w http.ResponseWriter, r *http.Request, id uuid.UUID, errorCode string) {
	people, err := h.personRepo.GetAll(r.Context())
	if err != nil {
		slog.Error("failed to list people", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	var person *model.Person
	for _, p := range people {
		if p.ID == id {
			person = p
			break
		}
	}
	if person == nil {
		w.Header().Set("HX-Redirect", "/people?error=not_found")
		return
	}

	form := pages.PeopleFormData{Name: person.Name, Color: person.Color}
	if errorCode != "" {
		form = pages.PeopleFormData{Name: r.PostFormValue("name"), Color: r.PostFormValue("color")}
	} else {
		w.Header().Set("HX-Trigger", `{"showToast": {"message": "Saved!", "type": "success"}}`)
	}
	if err := pages.PersonCard(person, form, peopleErrors[errorCode]).Render(r.Context(), w); err != nil {
		slog.Error("failed to render person card", "error", err)
	}
}

// Delete removes a person who has no ratings or picks.
func (h *PeopleHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "Invalid person ID", http.StatusBadRequest)
		return
	}

	err = h.personRepo.Delete(r.Context(), id)
	if errors.Is(err, repository.ErrPersonNotFound) {
		redirectPeople(w, r, "not_found")
		return
	}
	if errors.Is(err, repository.ErrPersonInUse) {
		redirectPeople(w, r, "in_use")
		return
	}
	if err != nil {
		slog.Error("failed to delete person", "error", err)
		http.Error(w, "Failed to remove person", http.StatusInternalServerError)
		return
	}

	slog.Info("person deleted", "person_id", id)
	redirectPeople(w, r, "")
}

func redirectPeople(w http.ResponseWriter, r *http.Request, errorCode string) {
	target := "/people"
	if errorCode != "" {
		target += "?error=" + errorCode
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// cleanPersonName trims name and reports whether it is 1 to 50 characters of
// valid UTF-8 with no control characters.
func cleanPersonName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	ok := utf8.ValidString(name) && n > 0 && n <= maxPersonNameLength && strings.IndexFunc(name, unicode.IsControl) < 0
	return name, ok
}

// cleanPersonInitial upper-cases initial and reports whether it is one letter.
func cleanPersonInitial(initial string) (string, bool) {
	r, size := utf8.DecodeRuneInString(initial)
	if r == utf8.RuneError || size != len(initial) || !unicode.IsLetter(r) {
		return "", false
	}
	return string(unicode.ToUpper(r)), true
}

// suggestColor returns the first palette colour nobody uses, or, once all are
// taken, cycles through the palette by head count.
func suggestColor(people []*model.Person) string {
	used := make(map[string]bool, len(people))
	for _, person := range people {
		used[person.Color] = true
	}
	for _, color := range model.PersonColors {
		if !used[color] {
			return color
		}
	}
	return model.PersonColors[len(people)%len(model.PersonColors)]
}
