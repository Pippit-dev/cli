//go:build !linux && !darwin && !windows

package trae

import "context"

func parentExecutable(context.Context, int) (int, string) { return 0, "" }
