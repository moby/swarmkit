package log

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoggerContext(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, GetLogger(ctx), L)      // should be same as L variable
	assert.Equal(t, G(ctx), GetLogger(ctx)) // these should be the same.

	ctx = WithLogger(ctx, G(ctx).WithField("test", "one"))
	assert.Equal(t, "one", GetLogger(ctx).Data["test"])
	assert.Equal(t, G(ctx), GetLogger(ctx)) // these should be the same.
}

func TestModuleContext(t *testing.T) {
	ctx := context.Background()
	assert.Empty(t, GetModulePath(ctx))

	ctx = WithModule(ctx, "a") // basic behavior
	assert.Equal(t, "a", GetModulePath(ctx))
	logger := GetLogger(ctx)
	assert.Equal(t, "a", logger.Data["module"])

	parent, ctx := ctx, WithModule(ctx, "a")
	assert.Equal(t, ctx, parent) // should be a no-op
	assert.Equal(t, "a", GetModulePath(ctx))
	assert.Equal(t, "a", GetLogger(ctx).Data["module"])

	ctx = WithModule(ctx, "b") // new module
	assert.Equal(t, "a/b", GetModulePath(ctx))
	assert.Equal(t, "a/b", GetLogger(ctx).Data["module"])

	ctx = WithModule(ctx, "c") // new module
	assert.Equal(t, "a/b/c", GetModulePath(ctx))
	assert.Equal(t, "a/b/c", GetLogger(ctx).Data["module"])
}
