package util

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"

	"github.com/google/shlex"
)

// split str at first space character, return first and others part, both TrimSpaced
func SplitFirstAndOthers(str string) (first string, others string) {
	// unicode.IsSpace
	i := strings.IndexAny(str, " \r\n\t\f")
	if i != -1 {
		first = str[:i]
		others = strings.TrimSpace(str[i+1:])
	} else {
		first = strings.TrimSpace(str)
	}
	return
}

func UniqueSlice[T comparable](s []T, except *T) (list []T) {
	keys := map[T]bool{}
	for _, entry := range s {
		if !keys[entry] || (except != nil && entry == *except) {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return
}

func UniqueSliceInLastOrder[T comparable](s []T, except *T) (list []T) {
	list = append(list, s...)
	slices.Reverse(list)
	list = UniqueSlice(list, except)
	slices.Reverse(list)
	return
}

func SliceLastIndex[T comparable](s []T, v T) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == v {
			return i
		}
	}
	return -1
}

func SliceLastIndexFunc[T comparable](s []T, test func(T) bool) int {
	for i := len(s) - 1; i >= 0; i-- {
		if test(s[i]) {
			return i
		}
	}
	return -1
}

// return a ctx that is automatcally cancelled if sign received a data
func ContextWithCancelSign(ctx context.Context, sign <-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	go func(ctx context.Context, sign <-chan struct{}, cancel context.CancelFunc) {
		for {
			select {
			case <-sign:
				cancel()
			case <-ctx.Done():
			}
		}
	}(ctx, sign, cancel)
	return ctx, cancel
}

// Append els to s, then return the last cap unique elements of result
func AppendUniqueCapSlice[T comparable](s []T, cap int, els ...T) (ns []T) {
	ns = append(s, els...)
	ns = UniqueSliceInLastOrder(ns, nil)
	if cap > 0 && len(ns) > cap {
		ns = ns[len(ns)-cap:]
	}
	return
}

// Split s to slice of string. Eath string in result slice has at most chunkSize UTF-8 characters.
// From https://stackoverflow.com/questions/25686109/split-string-by-length-in-golang
func Chunks(s string, chunkSize int) []string {
	if len(s) == 0 {
		return nil
	}
	if chunkSize >= len(s) {
		return []string{s}
	}
	var chunks []string = make([]string, 0, (len(s)-1)/chunkSize+1)
	currentLen := 0
	currentStart := 0
	for i := range s {
		if currentLen == chunkSize {
			chunks = append(chunks, s[currentStart:i])
			currentLen = 0
			currentStart = i
		}
		currentLen++
	}
	chunks = append(chunks, s[currentStart:])
	return chunks
}

func Filter[T any](ss []T, test func(T) bool) (ret []T) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}

// Each line in "no data" format, find the first line which no equals index and return it's data
func FindLineDataByFirstField(lines []string, index string) string {
	for _, line := range lines {
		no, data := SplitFirstAndOthers(line)
		if no == index {
			return data
		}
	}
	return ""
}

// Run a cmd and return combined output.
// If args is not provided, treat cmd as a cmdline and parse it to extract cmd args
func RunCommand(cmd string, args ...string) (result string, err error) {
	if len(args) == 0 {
		args, err = shlex.Split(cmd)
		if err != nil || len(args) == 0 || args[0] == "" {
			return "", fmt.Errorf("invalid cmdline %s: %v", cmd, err)
		}
		cmd = args[0]
		args = args[1:]
	}
	command := exec.Command(cmd, args...)
	data, err := command.CombinedOutput()
	result = string(data)
	return
}

// Change cwd to dir and return the new cwd.
// Try to parse real path of dir using system shell, so env expression can be used in dir.
// If dir is empty, use user home dir instead.
func Cd(dir string) (cwd string, err error) {
	if dir == "" {
		cwd, err = os.UserHomeDir()
	} else {
		if runtime.GOOS == "windows" {
			cwd, err = RunCommand("cmd", "/C", fmt.Sprintf("cd %s && cd", dir))
		} else {
			cwd, err = RunCommand("sh", "-c", fmt.Sprintf("cd %s; pwd", dir))
		}
		cwd = strings.TrimSpace(cwd)
	}
	if err == nil {
		err = os.Chdir(cwd)
	}
	return
}
