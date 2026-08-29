//go:build windows

package livegate

import (
	"errors"
	"fmt"
	"testing"

	"golang.org/x/sys/windows"
)

func TestClassifyBundleWindowsPublishErrorOnlyTreatsKnownCollisionsAsCollision(t *testing.T) {
	opaque := errors.New("opaque publish error")
	tests := []struct {
		name       string
		publishErr error
		collision  bool
	}{
		{name: "already exists", publishErr: windows.ERROR_ALREADY_EXISTS, collision: true},
		{name: "file exists", publishErr: windows.ERROR_FILE_EXISTS, collision: true},
		{
			name:       "wrapped already exists",
			publishErr: fmt.Errorf("move: %w", windows.ERROR_ALREADY_EXISTS),
			collision:  true,
		},
		{name: "access denied", publishErr: windows.ERROR_ACCESS_DENIED},
		{name: "sharing violation", publishErr: windows.ERROR_SHARING_VIOLATION},
		{name: "file not found", publishErr: windows.ERROR_FILE_NOT_FOUND},
		{name: "path not found", publishErr: windows.ERROR_PATH_NOT_FOUND},
		{name: "opaque", publishErr: opaque},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyBundleWindowsPublishError(test.publishErr)
			if test.collision {
				if !errors.Is(got, errBundleCollision) {
					t.Fatalf("classify error = %v, want collision", got)
				}
				return
			}
			if got != test.publishErr {
				t.Fatalf("classify error = %v, want exact original %v", got, test.publishErr)
			}
		})
	}
}
