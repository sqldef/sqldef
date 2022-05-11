// sqldef.go: sqldef's original code under the parser pacakge
// This file is subject to sqldef's own MIT lisence and NOT part of sqlparser's Apache License.
package parser

import (
	"regexp"
	"strings"
)

// A tuple of an original DDL and a Statement
type DDLStatement struct {
	DDL       string
	Statement Statement
}

func Parse(str string, mode ParserMode) ([]DDLStatement, error) {
	ddls, err := splitDDLs(str, mode)
	if err != nil {
		return nil, err
	}

	var result []DDLStatement
	for _, ddl := range ddls {
		ddl, _ = SplitMarginComments(ddl)
		stmt, err := ParseStrictDDLWithMode(ddl, mode)
		if err != nil {
			return result, err
		}
		result = append(result, DDLStatement{DDL: ddl, Statement: stmt})
	}
	return result, nil
}

func splitDDLs(str string, mode ParserMode) ([]string, error) {
	re := regexp.MustCompilePOSIX("^--.*")
	str = re.ReplaceAllString(str, "")

	ddls := strings.Split(str, ";")
	var result []string

	for len(ddls) > 0 {
		// SplitStatementToPieces() doesn't work well when there's a nested statement for a trigger.
		// So we just attempt parsing until it succeeds. I'll let the parser do it in the future.
		var ddl string
		var err error
		i := 1
		for {
			ddl = strings.Join(ddls[0:i], ";")
			ddl = strings.TrimSpace(ddl)
			ddl = strings.TrimSuffix(ddl, ";")
			if ddl == "" {
				break
			}

			ddl, _ = SplitMarginComments(ddl)
			_, err = ParseStrictDDLWithMode(ddl, mode)
			if err == nil || i == len(ddls) {
				break
			}
			i++
		}

		if err != nil {
			return result, err
		}
		if ddl != "" {
			result = append(result, ddl)
		}

		if i < len(ddls) {
			ddls = ddls[i:]
		} else {
			break
		}
	}
	return result, nil
}
