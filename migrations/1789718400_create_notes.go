package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Example collection. Every schema change should live in a migration so all
// environments converge on the same schema; in development the dashboard
// generates these files for you (automigrate).
func init() {
	m.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}

		notes := core.NewBaseCollection("notes")

		// API rules: each user can only see and modify their own notes.
		// A nil rule means superusers only.
		isOwner := "@request.auth.id != '' && owner = @request.auth.id"
		// The submitted owner may be omitted (hooks default it to the
		// requester) but can never point at someone else.
		ownerUnchanged := "(@request.body.owner:isset = false || @request.body.owner = @request.auth.id)"

		notes.ListRule = types.Pointer(isOwner)
		notes.ViewRule = types.Pointer(isOwner)
		notes.CreateRule = types.Pointer("@request.auth.id != '' && " + ownerUnchanged)
		notes.UpdateRule = types.Pointer(isOwner + " && " + ownerUnchanged)
		notes.DeleteRule = types.Pointer(isOwner)

		notes.Fields.Add(
			&core.RelationField{
				Name:          "owner",
				CollectionId:  users.Id,
				Required:      true,
				MaxSelect:     1,
				CascadeDelete: true,
			},
			&core.TextField{Name: "title", Required: true, Max: 200},
			&core.TextField{Name: "body", Max: 10000},
			&core.BoolField{Name: "pinned"},
			&core.AutodateField{Name: "created", OnCreate: true},
			&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
		)

		notes.AddIndex("idx_notes_owner_created", false, "owner, created", "")

		return app.Save(notes)
	}, func(app core.App) error {
		notes, err := app.FindCollectionByNameOrId("notes")
		if err != nil {
			return err
		}
		return app.Delete(notes)
	})
}
