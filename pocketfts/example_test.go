package pocketfts_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ireullin/pocket-fts/pocketfts"
)

// Example shows the whole life cycle of an embedded store: open, create a
// collection, write, search, delete, close.
func Example() {
	dir, err := os.MkdirTemp("", "pocketfts-example")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	ctx := context.Background()
	store, err := pocketfts.Open(pocketfts.Config{Path: filepath.Join(dir, "db.sqlite")})
	if err != nil {
		panic(err)
	}
	defer store.Close()

	err = store.CreateCollection(ctx, pocketfts.Schema{
		Name:       "notes",
		PrimaryKey: "id",
		Fields: []pocketfts.Field{
			{Name: "id", Type: "text"},
			{Name: "body", Type: "text", Searchable: true},
			{Name: "status", Type: "text"},
		},
	})
	if err != nil {
		panic(err)
	}

	for _, doc := range []pocketfts.Document{
		{"id": "n1", "body": "季度 預算 會議", "status": "done"},
		{"id": "n2", "body": "預算 草案", "status": "draft"},
		{"id": "n3", "body": "午餐 菜單", "status": "done"},
	} {
		if err := store.Upsert(ctx, "notes", doc); err != nil {
			panic(err)
		}
	}

	query, err := pocketfts.ParseNode([]byte(`{"$and": [
		{"search": {"term": "預算"}},
		{"sql": {"where": {"status": "done"}}}
	]}`))
	if err != nil {
		panic(err)
	}
	rows, err := store.Query(ctx, "notes", query, pocketfts.Result{Fields: []string{"id"}})
	if err != nil {
		panic(err)
	}
	for _, row := range rows {
		fmt.Println(row["id"])
	}

	if err := store.Delete(ctx, "notes", "n1"); err != nil {
		panic(err)
	}
	rows, err = store.Query(ctx, "notes", query, pocketfts.Result{})
	if err != nil {
		panic(err)
	}
	fmt.Println(len(rows))

	// Output:
	// n1
	// 0
}
