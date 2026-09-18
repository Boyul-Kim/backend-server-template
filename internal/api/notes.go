package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	validation "github.com/pocketbase/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// Example endpoints for the `notes` collection (see migrations/). Replace
// them with your own domain.

type noteStats struct {
	Total  int64 `json:"total"`
	Pinned int64 `json:"pinned"`
}

// handleNoteStats: GET /api/v1/notes/stats
func handleNoteStats(e *core.RequestEvent) error {
	owner := dbx.HashExp{"owner": e.Auth.Id}

	total, err := e.App.CountRecords("notes", owner)
	if err != nil {
		return err
	}
	pinned, err := e.App.CountRecords("notes", owner, dbx.HashExp{"pinned": true})
	if err != nil {
		return err
	}

	return e.JSON(http.StatusOK, noteStats{Total: total, Pinned: pinned})
}

const maxImport = 100

type importNote struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	Pinned bool   `json:"pinned"`
}

func (n importNote) Validate() error {
	return validation.ValidateStruct(&n,
		validation.Field(&n.Title, validation.Required, validation.Length(1, 200)),
		validation.Field(&n.Body, validation.Length(0, 10000)),
	)
}

type importRequest struct {
	Notes []importNote `json:"notes"`
}

func (r importRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Notes, validation.Required, validation.Length(1, maxImport)),
	)
}

type importResponse struct {
	IDs []string `json:"ids"`
}

// handleNoteImport: POST /api/v1/notes/import
//
// Creates up to 100 notes for the current user atomically: either every note
// is created or none is.
func handleNoteImport(e *core.RequestEvent) error {
	var req importRequest
	if err := e.BindBody(&req); err != nil {
		return apis.NewBadRequestError("Invalid request body.", err)
	}
	for i := range req.Notes {
		req.Notes[i].Title = strings.TrimSpace(req.Notes[i].Title)
	}
	if err := req.Validate(); err != nil {
		return apis.NewBadRequestError("Validation failed.", err)
	}

	collection, err := e.App.FindCachedCollectionByNameOrId("notes")
	if err != nil {
		return err
	}

	ids := make([]string, 0, len(req.Notes))
	err = e.App.RunInTransaction(func(txApp core.App) error {
		for _, n := range req.Notes {
			record := core.NewRecord(collection)
			record.Set("owner", e.Auth.Id)
			record.Set("title", n.Title)
			record.Set("body", n.Body)
			record.Set("pinned", n.Pinned)

			if err := txApp.Save(record); err != nil {
				return err
			}
			ids = append(ids, record.Id)
		}
		return nil
	})
	if err != nil {
		var verr validation.Errors
		if errors.As(err, &verr) {
			return apis.NewBadRequestError("Failed to import notes.", verr)
		}
		return err
	}

	return e.JSON(http.StatusCreated, importResponse{IDs: ids})
}
