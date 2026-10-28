package board

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// upsert finds the item of b[key] that match accepts (appending ident when
// there is none) and sets fields on it; an empty value removes the field.
func upsert(dir, key string, match func(obj) bool, ident map[string]any, fields map[string]string) error {
	return EditBoard(dir, func(b obj) error {
		l, err := list(b, key)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(l, match)
		if i < 0 {
			o := obj{}
			for k, v := range ident {
				set(o, k, v)
			}
			l, i = append(l, o), len(l)
		}
		for k, v := range fields {
			setOrDelete(l[i], k, v)
		}
		setList(b, key, l)
		return nil
	})
}

// remove drops every item of b[key] that match accepts; what names it in
// the error when there is none.
func remove(dir, key string, match func(obj) bool, what string) error {
	return EditBoard(dir, func(b obj) error {
		l, err := list(b, key)
		if err != nil {
			return err
		}
		n := len(l)
		if l = slices.DeleteFunc(l, match); len(l) == n {
			return errors.New("no " + what)
		}
		setList(b, key, l)
		return nil
	})
}

// prEnums are the allowed values of prs[] fields ("" removes the field).
var prEnums = map[string][]string{
	"state":  {"open", "draft", "merged", "closed", ""},
	"ci":     {"pass", "fail", "pending", ""},
	"review": {"approved", "changes", "pending", ""},
}

// ParsePRRef splits `repo#n`.
func ParsePRRef(ref string) (string, int, error) {
	i := strings.LastIndexByte(ref, '#')
	n, err := strconv.Atoi(ref[i+1:])
	if i <= 0 || err != nil || n <= 0 {
		return "", 0, fmt.Errorf("want repo#n, got %q", ref)
	}
	return ref[:i], n, nil
}

func matchPR(repo string, n int) func(obj) bool {
	return func(o obj) bool {
		var on int
		_ = json.Unmarshal(o["n"], &on)
		return str(o, "repo") == repo && on == n
	}
}

// SetPR upserts the PR repo#n with fields (title, url, state, ci, review).
func SetPR(dir, repo string, n int, fields map[string]string) error {
	for k, v := range fields {
		if allowed, ok := prEnums[k]; ok {
			if err := checkEnum(k, v, allowed); err != nil {
				return err
			}
		}
	}
	return upsert(dir, "prs", matchPR(repo, n), map[string]any{"repo": repo, "n": n}, fields)
}

// RmPR removes the PR repo#n.
func RmPR(dir, repo string, n int) error {
	return remove(dir, "prs", matchPR(repo, n), fmt.Sprintf("PR %s#%d", repo, n))
}
