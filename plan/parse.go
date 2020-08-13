package plan

import (
	"fmt"
	"strings"

	"github.com/pingcap/errors"
)

func ParseText(sql, explainText string) (Plan, error) {
	explainLines, err := trimAndSplitExplainResult(explainText)
	if err != nil {
		return Plan{}, err
	}
	sql = strings.TrimSpace(sql)
	sql = strings.TrimSuffix(sql, ";")
	ver := identifyVersion(explainLines[1])
	rows := splitRows(explainLines[3 : len(explainLines)-1])
	return Parse(ver, sql, rows)
}

func Parse(version, sql string, explainRows [][]string) (Plan, error) {
	ver := matchVersion(version)
	if ver == VUnknown {
		return Plan{}, fmt.Errorf("unknown version %v", version)
	}
	switch ver {
	case V3:
		return ParseV3(sql, explainRows)
	case V4:
		return ParseV4(sql, explainRows)
	}
	return Plan{}, errors.Errorf("unsupported TiDB version %v", ver)
}

func Compare(p1, p2 Plan) (reason string, same bool) {
	if p1.SQL != p2.SQL {
		return "differentiate SQLs", false
	}
	return compare(p1.Root, p2.Root)
}

func compare(op1, op2 Operator) (reason string, same bool) {
	if op1.Type() != op2.Type() || op1.Task() != op2.Task() {
		return fmt.Sprintf("%v and %v have different types", op1.ID(), op2.ID()), false
	}
	c1, c2 := op1.Children(), op2.Children()
	if len(c1) != len(c2) {
		return fmt.Sprintf("%v and %v have different children lengths", op1.ID(), op2.ID()), false
	}
	same = true
	switch op1.Type() {
	case OpTypeTableScan:
		t1, t2 := op1.(TableScanOp), op2.(TableScanOp)
		if t1.Table != t2.Table {
			same = false
			reason = fmt.Sprintf("%v:%v, %v:%v", t1.ID(), t1.Table, t2.ID(), t2.Table)
		}
	case OpTypeIndexScan:
		t1, t2 := op1.(IndexScanOp), op2.(IndexScanOp)
		if t1.Table != t2.Table || t1.Index != t2.Index {
			same = false
			reason = fmt.Sprintf("%v:%v, %v:%v", t1.ID(), t1.Table, t2.ID(), t2.Table)
		}
	}
	if !same {
		return reason, false
	}
	for i := range c1 {
		if reason, same = compare(c1[i], c2[i]); !same {
			return reason, same
		}
	}
	return "", true
}

func trimAndSplitExplainResult(explainResult string) ([]string, error) {
	lines := strings.Split(explainResult, "\n")
	var idx [3]int
	p := 0
	for i := range lines {
		if isSeparateLine(lines[i]) {
			idx[p] = i
			p++
			if p == 3 {
				break
			}
		}
	}
	if p != 3 {
		return nil, errors.Errorf("invalid explain result")
	}
	return lines[idx[0] : idx[2]+1], nil
}

func isSeparateLine(line string) bool {
	line = strings.TrimSpace(line)
	if len(line) == 0 {
		return false
	}
	for _, c := range line {
		if c != '+' && c != '-' {
			return false
		}
	}
	return true
}

func matchVersion(version string) string {
	v := strings.ToLower(version)
	if strings.Contains(v, "v3") {
		return V3
	} else if strings.Contains(v, "v4") {
		return V4
	}
	return VUnknown
}

func identifyVersion(header string) string {
	if strings.Contains(header, "estRows") {
		return V4
	}
	return V3
}

func splitRows(rows []string) [][]string {
	results := make([][]string, 0, len(rows))
	for _, row := range rows {
		cols := strings.Split(row, "|")
		cols = cols[1 : len(cols)-1]
		results = append(results, cols)
	}
	return results
}

func findChildRowNo(rows [][]string, parentRowNo, idColNo int) []int {
	parent := []rune(rows[parentRowNo][idColNo])
	col := 0
	for col = range parent {
		c := parent[col]
		if c >= 'A' && c <= 'Z' {
			break
		}
	}
	if col >= len(parent) {
		return nil
	}
	childRowNo := make([]int, 0, 2)
	for i := parentRowNo + 1; i < len(rows); i++ {
		field := rows[i][idColNo]
		c := []rune(field)[col]
		if c == '├' || c == '└' {
			childRowNo = append(childRowNo, i)
		} else if c != '│' {
			break
		}
	}
	return childRowNo
}

func extractOperatorID(field string) string {
	return strings.TrimFunc(field, func(c rune) bool {
		return c == '└' || c == '─' || c == '│' || c == '├' || c == ' '
	})
}

func splitKVs(kvStr string) map[string]string {
	kvMap := make(map[string]string)
	kvs := strings.Split(kvStr, ",")
	for _, kv := range kvs {
		fields := strings.Split(kv, ":")
		if len(fields) == 2 {
			kvMap[strings.TrimSpace(fields[0])] = strings.TrimSpace(fields[1])
		}
	}
	return kvMap
}

func parseTaskType(taskStr string) TaskType {
	task := strings.TrimSpace(strings.ToLower(taskStr))
	if task == "root" {
		return TaskTypeRoot
	}
	if strings.Contains(task, "tiflash") {
		return TaskTypeTiFlash
	}
	return TaskTypeTiKV
}

func MatchOpType(opID string) OpType {
	x := strings.ToLower(opID)
	if strings.Contains(x, "join") {
		if strings.Contains(x, "hash") {
			return OpTypeHashJoin
		} else if strings.Contains(x, "merge") {
			return OpTypeMergeJoin
		} else if strings.Contains(x, "index") {
			return OpTypeIndexJoin
		}
		return OpTypeUnknown
	}
	if strings.Contains(x, "table") {
		if strings.Contains(x, "reader") {
			return OpTypeTableReader
		} else if strings.Contains(x, "scan") {
			return OpTypeTableScan
		}
		return OpTypeUnknown
	}
	if strings.Contains(x, "index") {
		if strings.Contains(x, "reader") {
			return OpTypeIndexReader
		} else if strings.Contains(x, "scan") {
			return OpTypeIndexScan
		} else if strings.Contains(x, "lookup") {
			return OpTypeIndexLookup
		}
		return OpTypeUnknown
	}
	if strings.Contains(x, "selection") {
		return OpTypeSelection
	}
	if strings.Contains(x, "projection") {
		return OpTypeProjection
	}
	if strings.Contains(x, "point") {
		return OpTypePointGet
	}
	return OpTypeUnknown
}
