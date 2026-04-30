package luadebug_test

import (
	"testing"

	lua "github.com/akzj/go-lua/pkg/lua"
	"github.com/lumi-so/lumi/pkg/luadebug"
)

func TestGetMemoryInfo(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	info := luadebug.GetMemoryInfo(L)
	if info.UsedBytes <= 0 {
		t.Errorf("memory used = %d, expected > 0", info.UsedBytes)
	}
	if info.GCMode == "" {
		t.Error("GCMode is empty")
	}
	t.Logf("Memory: %d bytes, GC mode: %s", info.UsedBytes, info.GCMode)
}
