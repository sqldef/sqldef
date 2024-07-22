package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStringConstantSimple(t *testing.T) {
	assert.Equal(t, StringConstant(""), "''")
	assert.Equal(t, StringConstant("hello world"), "'hello world'")
}

func TestStringConstantContainingSingleQuote(t *testing.T) {
	assert.Equal(t, StringConstant("it's the bee's knees"), "'it''s the bee''s knees'")
	assert.Equal(t, StringConstant("'"), "''''")
	assert.Equal(t, StringConstant("''"), "''''''")
	assert.Equal(t, StringConstant("'example'"), "'''example'''")
}
