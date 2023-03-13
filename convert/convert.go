package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	hcl "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	ctyconvert "github.com/zclconf/go-cty/cty/convert"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

type Options struct {
	Simplify bool
}

func String(filename string) (map[string]interface{}, error) {
	//buffer := bytes.NewBuffer([]byte{})
	var options Options

	data := make(map[string]interface{})

	// // Read input file
	// var stream io.Reader
	// file, err := os.Open(filename)
	// if err != nil {
	// 	return nil, fmt.Errorf("convert to json: %w", err)
	// }
	// defer file.Close()
	// stream = file
	// _, err = buffer.ReadFrom(stream)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to read files. Error : %w", err)
	// }
	// buffer.WriteByte('\n') // just in case it doesn't have an ending newline

	converted, err := Bytes([]byte(filename), "", options)
	if err != nil {

		return nil, fmt.Errorf("failed to convert file: %w", err)
	}

	data["json"] = string(converted)
	//data["lines"] = string(lineInfo)
	return data, nil

}

// Bytes takes the contents of an HCL file, as bytes, and converts
// them into a JSON representation of the HCL file.
func Bytes(bytes []byte, filename string, options Options) ([]byte, error) {
	file, diags := hclsyntax.ParseConfig(bytes, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parse config: %v", diags.Errs())
	}

	hclBytes, err := File(file, options)
	if err != nil {
		return nil, fmt.Errorf("convert to HCL: %w", err)
	}

	return hclBytes, nil
}

// File takes an HCL file and converts it to its JSON representation.
func File(file *hcl.File, options Options) ([]byte, error) {
	convertedFile, err := ConvertFile(file, options)
	if err != nil {
		return nil, fmt.Errorf("convert file: %w", err)
	}

	jsonBytes, err := json.Marshal(convertedFile)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	//lineBytes, err := json.Marshal(lineObj)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}

	return jsonBytes, nil
}

type jsonObj = map[string]interface{}
type lineObj = map[string]interface{}

type converter struct {
	bytes   []byte
	options Options
}

func ConvertFile(file *hcl.File, options Options) (jsonObj, error) {
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("convert file body to body type")
	}

	c := converter{
		bytes:   file.Bytes,
		options: options,
	}

	out, err := c.convertBody(body)
	if err != nil {
		return nil, fmt.Errorf("convert body: %w", err)
	}

	return out, nil
}

func (c *converter) convertBody(body *hclsyntax.Body) (jsonObj, error) {

	var (
		err  error
		cfg  = make(jsonObj) // resource config
		lcfg = make(lineObj) // resource line config
	)

	// convert attributes
	for key, value := range body.Attributes {
		cfg[key], lcfg[key], err = c.convertExpression(value.Expr)
		if err != nil {
			return nil, err
		}
	}

	// convert blocks (nested objects, lists)
	for _, block := range body.Blocks {

		var (
			bcfg  = make(jsonObj) // block resource config
			blcfg = make(lineObj) // block resource line config
		)

		err = c.convertBlock(block, bcfg, blcfg)
		if err != nil {
			return nil, err
		}

		blockConfig := bcfg[block.Type].(jsonObj)
		lineCfg := blcfg[block.Type].(lineObj)
		if _, present := cfg[block.Type]; !present {
			cfg[block.Type] = []jsonObj{blockConfig}
			lcfg[block.Type] = []lineObj{lineCfg}
		} else {
			list := cfg[block.Type].([]jsonObj)
			list = append(list, blockConfig)
			cfg[block.Type] = list

			lineList := lcfg[block.Type].([]lineObj)
			lineList = append(lineList, lineCfg)
			lcfg[block.Type] = lineList
		}
	}

	return cfg, nil
}

func (c *converter) rangeSource(r hcl.Range) string {
	// for some reason the range doesn't include the ending paren, so
	// check if the next character is an ending paren, and include it if it is.
	end := r.End.Byte
	if end < len(c.bytes) && c.bytes[end] == ')' {
		end++
	}
	return string(c.bytes[r.Start.Byte:end])
}

func (c *converter) convertBlock(block *hclsyntax.Block, cfg jsonObj, lcfg lineObj) error {
	var key string = block.Type

	value, err := c.convertBody(block.Body)
	if err != nil {
		return err
	}

	for _, label := range block.Labels {
		if inner, exists := cfg[key]; exists {
			var ok bool
			cfg, ok = inner.(jsonObj)
			if !ok {
				// TODO: better diagnostics
				return fmt.Errorf("unable to convert Block to JSON: %v.%v", block.Type, strings.Join(block.Labels, "."))
			}

			if innerLineObj := lcfg[key]; exists {
				lcfg, ok = innerLineObj.(lineObj)
				if !ok {
					return fmt.Errorf("unable to convert Block to JSON: %v.%v", block.Type, strings.Join(block.Labels, "."))
				}
			}

		} else {
			var (
				obj  = make(jsonObj)
				lobj = make(lineObj)
			)

			cfg[key] = obj
			cfg = obj

			lcfg[key] = lobj
			lcfg = lobj

		}
		key = label
	}

	// resource config for blocks
	if current, exists := cfg[key]; exists {
		if list, ok := current.([]interface{}); ok {
			cfg[key] = append(list, value)
		} else {
			cfg[key] = []interface{}{current, value}
		}
	} else {
		cfg[key] = value
	}

	// resource line config for blocks
	// if current, exists := lcfg[key]; exists {
	// 	if list, ok := current.([]interface{}); ok {
	// 		lcfg[key] = append(list, blcfg)
	// 	} else {
	// 		lcfg[key] = []interface{}{current, blcfg}
	// 	}
	// } else {
	// 	lcfg[key] = blcfg
	// }

	return nil
}

func (c *converter) convertExpression(expr hclsyntax.Expression) (ret interface{}, line interface{}, err error) {
	lineInfo := make(map[string]int)

	lineInfo["line"] = expr.StartRange().Start.Line
	lineInfo["startIndex"] = expr.StartRange().Start.Column
	lineInfo["endIndex"] = expr.StartRange().End.Column

	line = lineInfo

	switch value := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		return ctyjson.SimpleJSONValue{Value: value.Val}, line, nil
	case *hclsyntax.TemplateExpr:
		ret, err = c.convertTemplate(value)
		return
	case *hclsyntax.TemplateWrapExpr:
		return c.convertExpression(value.Wrapped)
	case *hclsyntax.TupleConsExpr:
		var list []interface{}
		for _, ex := range value.Exprs {
			elem, line, err := c.convertExpression(ex)
			if err != nil {
				return nil, line, err
			}
			list = append(list, elem)
		}
		return list, line, nil
	case *hclsyntax.ObjectConsExpr:
		m := make(jsonObj)
		l := make(lineObj)
		for _, item := range value.Items {
			key, err := c.convertKey(item.KeyExpr)
			if err != nil {
				return nil, line, err
			}
			m[key], l[key], err = c.convertExpression(item.ValueExpr)
			if err != nil {
				return nil, line, err
			}
		}
		return m, l, nil
	default:
		return c.wrapExpr(expr), line, nil
	}
}

func (c *converter) convertUnary(v *hclsyntax.UnaryOpExpr) (interface{}, error) {
	_, isLiteral := v.Val.(*hclsyntax.LiteralValueExpr)
	if !isLiteral {
		// If the expression after the operator isn't a literal, fall back to
		// wrapping the expression with ${...}
		return c.wrapExpr(v), nil
	}
	val, err := v.Value(nil)
	if err != nil {
		return nil, err
	}
	return ctyjson.SimpleJSONValue{Value: val}, nil
}

func (c *converter) convertTemplate(t *hclsyntax.TemplateExpr) (string, error) {
	if t.IsStringLiteral() {
		// safe because the value is just the string
		v, err := t.Value(nil)
		if err != nil {
			return "", err
		}
		return v.AsString(), nil
	}
	var builder strings.Builder
	for _, part := range t.Parts {
		s, err := c.convertStringPart(part)
		if err != nil {
			return "", err
		}
		builder.WriteString(s)
	}
	return builder.String(), nil
}

func (c *converter) convertStringPart(expr hclsyntax.Expression) (string, error) {
	switch v := expr.(type) {
	case *hclsyntax.LiteralValueExpr:
		// If the key is a bare "null", then we end up with null here,
		// in this case we should just return the string "null"
		if v.Val.IsNull() {
			return "null", nil
		}
		s, err := ctyconvert.Convert(v.Val, cty.String)
		if err != nil {
			return "", err
		}
		return s.AsString(), nil
	case *hclsyntax.TemplateExpr:
		return c.convertTemplate(v)
	case *hclsyntax.TemplateWrapExpr:
		return c.convertStringPart(v.Wrapped)
	case *hclsyntax.ConditionalExpr:
		return c.convertTemplateConditional(v)
	case *hclsyntax.TemplateJoinExpr:
		return c.convertTemplateFor(v.Tuple.(*hclsyntax.ForExpr))
	default:
		// treating as an embedded expression
		return c.wrapExpr(expr), nil
	}
}

func (c *converter) convertKey(keyExpr hclsyntax.Expression) (string, error) {
	// a key should never have dynamic input
	if k, isKeyExpr := keyExpr.(*hclsyntax.ObjectConsKeyExpr); isKeyExpr {
		keyExpr = k.Wrapped
		if _, isTraversal := keyExpr.(*hclsyntax.ScopeTraversalExpr); isTraversal {
			return c.rangeSource(keyExpr.Range()), nil
		}
	}
	return c.convertStringPart(keyExpr)
}

func (c *converter) convertTemplateConditional(expr *hclsyntax.ConditionalExpr) (string, error) {
	var builder strings.Builder
	builder.WriteString("%{if ")
	builder.WriteString(c.rangeSource(expr.Condition.Range()))
	builder.WriteString("}")
	trueResult, err := c.convertStringPart(expr.TrueResult)
	if err != nil {
		return "", nil
	}
	builder.WriteString(trueResult)
	falseResult, err := c.convertStringPart(expr.FalseResult)
	if len(falseResult) > 0 {
		builder.WriteString("%{else}")
		builder.WriteString(falseResult)
	}
	builder.WriteString("%{endif}")

	return builder.String(), nil
}

func (c *converter) convertTemplateFor(expr *hclsyntax.ForExpr) (string, error) {
	var builder strings.Builder
	builder.WriteString("%{for ")
	if len(expr.KeyVar) > 0 {
		builder.WriteString(expr.KeyVar)
		builder.WriteString(", ")
	}
	builder.WriteString(expr.ValVar)
	builder.WriteString(" in ")
	builder.WriteString(c.rangeSource(expr.CollExpr.Range()))
	builder.WriteString("}")
	templ, err := c.convertStringPart(expr.ValExpr)
	if err != nil {
		return "", err
	}
	builder.WriteString(templ)
	builder.WriteString("%{endfor}")

	return builder.String(), nil
}

func (c *converter) wrapExpr(expr hclsyntax.Expression) string {
	return "${" + c.rangeSource(expr.Range()) + "}"
}
