package tagio

import (
	"fmt"
	"strings"

	"go.senan.xyz/taglib"
)

// go-taglib runs TagLib in WebAssembly and panics when a call fails inside
// the module. Every call goes through guard, so one bad file fails alone
// instead of taking the server down.
func guard[T any](op, path string, fn func() (T, error)) (v T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s %s: taglib panicked: %v", op, path, r)
		}
	}()
	v, err = fn()
	if err != nil {
		err = fmt.Errorf("%s %s: %w", op, path, err)
	}
	return v, err
}

func readTags(path string) (map[string][]string, error) {
	return guard("read tags", path, func() (map[string][]string, error) { return taglib.ReadTags(path) })
}

func readProperties(path string) (taglib.Properties, error) {
	return guard("read properties", path, func() (taglib.Properties, error) { return taglib.ReadProperties(path) })
}

func readImage(path string, index int) ([]byte, error) {
	return guard("read picture", path, func() ([]byte, error) { return taglib.ReadImageOptions(path, index) })
}

// writeTags changes only the keys in ops; a key with no values is removed.
// It never passes taglib.Clear, which drops every key not in the map.
func writeTags(path string, ops map[string][]string) error {
	_, err := guard("write tags", path, func() (struct{}, error) { return struct{}{}, taglib.WriteTags(path, ops, 0) })
	return err
}

// writeImage replaces the picture at index, appends it when index is past
// the end, or removes the picture at index when data is nil.
func writeImage(path string, data []byte, index int, mime string) error {
	_, err := guard("write picture", path, func() (struct{}, error) {
		return struct{}{}, taglib.WriteImageOptions(path, data, index, frontCover, "", mime)
	})
	return err
}

// frontCover is TagLib's name for the picture type shelf writes.
const frontCover = "Front Cover"

// validKey refuses keys go-taglib can't pass through: it joins a key and its
// values with a tab and the values with a vertical tab.
func validKey(k string) bool {
	return k != "" && !strings.ContainsAny(k, "\t\v\x00=")
}

// validValue refuses an empty value, which go-taglib reads as a removal, and
// the separators.
func validValue(v string) bool {
	return v != "" && !strings.ContainsAny(v, "\v\x00")
}
