/*
Copyright 2017 Google Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package parser

import (
	"strings"
	"unicode"
)

func isNonSpace(r rune) bool {
	return !unicode.IsSpace(r)
}

// leadingCommentEnd returns the first index after all leading comments, or
// 0 if there are no leading comments.
func leadingCommentEnd(text string) (end int) {
	hasComment := false
	pos := 0
	for pos < len(text) {
		// Eat up any whitespace. Trailing whitespace will be considered part of
		// the leading comments.
		nextVisibleOffset := strings.IndexFunc(text[pos:], isNonSpace)
		if nextVisibleOffset < 0 {
			break
		}
		pos += nextVisibleOffset
		remainingText := text[pos:]

		// Found visible characters. Look for '/*' at the beginning
		// and '*/' somewhere after that.
		if len(remainingText) < 4 || remainingText[:2] != "/*" {
			break
		}
		commentLength := 4 + strings.Index(remainingText[2:], "*/")
		if commentLength < 4 {
			// Missing end comment :/
			break
		}

		hasComment = true
		pos += commentLength
	}

	if hasComment {
		return pos
	}
	return 0
}

// trailingCommentStart returns the first index of trailing comments.
// If there are no trailing comments, returns the length of the input string.
func trailingCommentStart(text string) (start int) {
	hasComment := false
	reducedLen := len(text)
	for reducedLen > 0 {
		// Eat up any whitespace. Leading whitespace will be considered part of
		// the trailing comments.
		nextReducedLen := strings.LastIndexFunc(text[:reducedLen], isNonSpace) + 1
		if nextReducedLen == 0 {
			break
		}
		reducedLen = nextReducedLen
		if reducedLen < 4 || text[reducedLen-2:reducedLen] != "*/" {
			break
		}

		// Find the beginning of the comment
		startCommentPos := strings.LastIndex(text[:reducedLen-2], "/*")
		if startCommentPos < 0 {
			// Badly formatted sql :/
			break
		}

		hasComment = true
		reducedLen = startCommentPos
	}

	if hasComment {
		return reducedLen
	}
	return len(text)
}

// MarginComments holds the leading and trailing comments that surround a query.
type MarginComments struct {
	Leading  string
	Trailing string
}

// SplitMarginComments pulls out any leading or trailing comments from a raw sql query.
// This function also trims leading (if there's a comment) and trailing whitespace.
func SplitMarginComments(sql string) (query string, comments MarginComments) {
	trailingStart := trailingCommentStart(sql)
	leadingEnd := leadingCommentEnd(sql[:trailingStart])
	comments = MarginComments{
		Leading:  strings.TrimLeftFunc(sql[:leadingEnd], unicode.IsSpace),
		Trailing: strings.TrimRightFunc(sql[trailingStart:], unicode.IsSpace),
	}
	return strings.TrimFunc(sql[leadingEnd:trailingStart], unicode.IsSpace), comments
}

// ExtractMysqlComment extracts the version and SQL from a comment-only query
// such as /*!50708 sql here */
func ExtractMysqlComment(sql string) (version string, innerSQL string) {
	sql = sql[3 : len(sql)-2]

	digitCount := 0
	endOfVersionIndex := strings.IndexFunc(sql, func(c rune) bool {
		digitCount++
		return !unicode.IsDigit(c) || digitCount == 6
	})
	version = sql[0:endOfVersionIndex]
	innerSQL = strings.TrimFunc(sql[endOfVersionIndex:], unicode.IsSpace)

	return version, innerSQL
}
