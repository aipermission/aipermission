package sqlserverconnector

import (
	"context"
	"time"

	"github.com/antlr4-go/antlr/v4"
)

const executionParserTimeout = time.Second
const executionParserOperations = 500000

// Bound adaptive prediction as well as rule nesting. A depth counter alone
// does not constrain repeated shallow ambiguous statements before authorization.
type executionTokenStream struct {
	*antlr.CommonTokenStream
	ctx        context.Context
	operations int
}

func (s *executionTokenStream) check() {
	s.operations++
	if s.operations > executionParserOperations || s.ctx.Err() != nil {
		panic("execute_sql parser work budget exhausted")
	}
}

func (s *executionTokenStream) LA(i int) int {
	s.check()
	return s.CommonTokenStream.LA(i)
}

func (s *executionTokenStream) LT(i int) antlr.Token {
	s.check()
	return s.CommonTokenStream.LT(i)
}

func (s *executionTokenStream) Seek(index int) {
	s.check()
	s.CommonTokenStream.Seek(index)
}

func (s *executionTokenStream) Consume() {
	s.check()
	s.CommonTokenStream.Consume()
}
