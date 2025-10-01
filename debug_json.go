package main

import (
	"encoding/json"
	"fmt"
)

type Field struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Weight int    `json:"weight,omitempty"`
}

type FTSConfig struct {
	Stemming bool `json:"stemming"`
}

type CollectionSchema struct {
	Name       string    `json:"name"`
	PrimaryKey string    `json:"primary_key"`
	FTS        FTSConfig `json:"fts"`
	Fields     []Field   `json:"fields"`
}

func main() {
	schema := CollectionSchema{
		Name:       "products",
		PrimaryKey: "id",
		FTS: FTSConfig{
			Stemming: true,
		},
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "type", Type: "text", Weight: 1},
			{Name: "category", Type: "text", Weight: 2},
			{Name: "item", Type: "text", Weight: 3},
			{Name: "price", Type: "integer"},
			{Name: "weight", Type: "real"},
		},
	}
	
	jsonData, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	
	fmt.Printf("JSON that will be sent:\n%s\n", string(jsonData))
	
	// Test each field type
	for _, field := range schema.Fields {
		fmt.Printf("Field: %s, Type: %s\n", field.Name, field.Type)
	}
}