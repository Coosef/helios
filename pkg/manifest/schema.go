package manifest

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaID = "manifest.schema.json"

// schemaJSON is the embedded source of truth for the update-manifest schema. The
// published copy at api/manifest.schema.json must match it (drift-tested).
//
//go:embed manifest.schema.json
var schemaJSON []byte

// SchemaBytes returns a copy of the embedded JSON Schema.
func SchemaBytes() []byte { return append([]byte(nil), schemaJSON...) }

var compiledSchema = mustCompileSchema()

func mustCompileSchema() *jsonschema.Schema {
	sch, err := compileSchema(schemaJSON)
	if err != nil {
		panic("manifest: embedded schema: " + err.Error())
	}
	return sch
}

func compileSchema(schemaBytes []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("invalid schema JSON: %w", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaID, doc); err != nil {
		return nil, fmt.Errorf("adding schema resource: %w", err)
	}
	sch, err := c.Compile(schemaID)
	if err != nil {
		return nil, fmt.Errorf("compiling schema: %w", err)
	}
	return sch, nil
}

// validateSchema validates raw manifest JSON against the embedded schema.
func validateSchema(data []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return compiledSchema.Validate(inst)
}
