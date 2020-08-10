package plan

import (
	"bytes"
	"fmt"
	"strings"
)

type PlanVer string

const (
	V3       = "v3"
	V4       = "v4"
	VUnknown = "unknown"
)

type OpType int

const (
	OpTypeUnknown OpType = iota
	OpTypeHashJoin
	OpTypeIndexJoin
	OpTypeMergeJoin
	OpTypeSelection
	OpTypeProjection
	OpTypeTableReader
	OpTypeTableScan
	OpTypeIndexReader
	OpTypeIndexScan
	OpTypeIndexLookup
)

func OpTypeIsJoin(opType OpType) bool {
	switch opType {
	case OpTypeIndexJoin, OpTypeMergeJoin, OpTypeHashJoin:
		return true
	}
	return false
}

type JoinType int

const (
	JoinTypeUnknown JoinType = iota
	JoinTypeInner
	JoinTypeLeftOuter
	JoinTypeRightOuter
	JoinTypeSemi
	JoinTypeAntiSemi
	JoinTypeLeftOuterSemi
	JoinTypeAntiLeftOuterSemi
)

type TaskType int

const (
	TaskTypeRoot TaskType = iota
	TaskTypeTiKV
	TaskTypeTiFlash
)

type Plan struct {
	SQL  string
	Ver  PlanVer
	Root Operator
}

func (p Plan) Format() string {
	buf := bytes.NewBuffer(nil)
	buf.WriteString("SQL: ")
	buf.WriteString(p.SQL)
	buf.WriteString("\n")
	buf.WriteString(p.Root.Format(0))
	return buf.String()
}

type Operator interface {
	ID() string
	Type() OpType
	EstRow() float64
	Task() TaskType

	Format(indent int) string
	Children() []Operator
}

type BaseOp struct {
	id     string
	opType OpType
	estRow float64
	task   TaskType

	children []Operator
}

func (op BaseOp) ID() string {
	return op.id
}

func (op BaseOp) Type() OpType {
	return op.opType
}

func (op BaseOp) EstRow() float64 {
	return op.estRow
}

func (op BaseOp) Task() TaskType {
	return op.task
}

func (op BaseOp) Format(indent int) string {
	buf := bytes.NewBuffer(nil)
	buf.WriteString(strings.Repeat(" ", indent))
	buf.WriteString(op.id)
	buf.WriteString("\t")
	buf.WriteString(fmt.Sprintf("%v", op.estRow))
	buf.WriteString("\n")
	for _, child := range op.children {
		buf.WriteString(child.Format(indent + 4))
	}
	return buf.String()
}

func (op BaseOp) Children() []Operator {
	return op.children
}

type HashJoinOp struct {
	BaseOp
	JoinType JoinType
}

type IndexJoinOp struct {
	BaseOp
	JoinType JoinType
}

type MergeJoinOp struct {
	BaseOp
	JoinType JoinType
}

type TableReaderOp struct {
	BaseOp
}

type TableScanOp struct {
	BaseOp
	Table string
}

type IndexReaderOp struct {
	BaseOp
}

type IndexScanOp struct {
	BaseOp
	Table string
	Index string
}

type IndexLookupOp struct {
	BaseOp
}

type SelectionOp struct {
	BaseOp
}

type ProjectionOp struct {
	BaseOp
}