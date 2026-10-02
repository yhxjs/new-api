package common

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/expr-lang/expr/vm"
)

// CompileBalanceQueryExtract applies the same scalar-expression restrictions
// at channel save time and query time. Collection loops, ranges, and variable
// declarations can amplify a bounded response into unbounded CPU or memory use.
func CompileBalanceQueryExtract(source string, env map[string]interface{}) (*vm.Program, error) {
	source = strings.TrimSpace(source)
	if len(source) > dto.MaxBalanceQueryExtractLength {
		return nil, fmt.Errorf("balance extract expression exceeds %d characters", dto.MaxBalanceQueryExtractLength)
	}
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, err
	}
	validator := balanceQueryExpressionValidator{}
	ast.Walk(&tree.Node, &validator)
	if validator.err != nil {
		return nil, validator.err
	}
	return expr.Compile(source, expr.AsFloat64(), expr.Env(env),
		expr.DisableAllBuiltins(), expr.EnableBuiltin("float"), expr.EnableBuiltin("int"))
}

type balanceQueryExpressionValidator struct {
	err error
}

func (v *balanceQueryExpressionValidator) Visit(node *ast.Node) {
	if v.err != nil {
		return
	}
	switch n := (*node).(type) {
	case *ast.BinaryNode:
		if n.Operator == ".." || n.Operator == "matches" {
			v.err = fmt.Errorf("balance extract expressions do not support %s", n.Operator)
		}
	case *ast.VariableDeclaratorNode, *ast.SequenceNode, *ast.ArrayNode, *ast.MapNode,
		*ast.SliceNode, *ast.PredicateNode, *ast.PointerNode:
		v.err = fmt.Errorf("balance extract expressions only support scalar field access, arithmetic, and conditions")
	case *ast.BuiltinNode:
		switch n.Name {
		case "float", "int", "max", "min", "abs", "ceil", "floor":
		default:
			v.err = fmt.Errorf("balance extract expressions do not support %s", n.Name)
		}
	case *ast.CallNode:
		callee, ok := n.Callee.(*ast.IdentifierNode)
		if !ok {
			v.err = fmt.Errorf("balance extract expressions only support direct scalar helper calls")
			return
		}
		switch callee.Value {
		case "max", "min", "abs", "ceil", "floor", "float", "int":
			return
		case "json":
		default:
			v.err = fmt.Errorf("balance extract expressions do not support %s", callee.Value)
			return
		}
		if len(n.Arguments) != 1 {
			v.err = fmt.Errorf("balance extract json() requires one literal field path")
			return
		}
		path, ok := n.Arguments[0].(*ast.StringNode)
		if !ok || strings.ContainsAny(path.Value, "@|()[]{}") {
			// GJSON modifiers and selectors can expand a small response into
			// unbounded output. Only static field/index paths are permitted.
			v.err = fmt.Errorf("balance extract json() only supports literal field paths without modifiers, pipes, or selectors")
		}
	}
}
