// Package builtin 提供内置工具实现。
package builtin

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"unicode"

	"genesis-agent/internal/capabilities/tool/contract"
	toolparam "genesis-agent/internal/capabilities/tool/param"
)

const (
	maxCalculatorExpressionRunes = 4096
	maxCalculatorDepth           = 128
	maxCalculatorNumberDigits    = 256
	maxCalculatorExponentAbs     = 1000
	defaultCalculatorScale       = 10
	maxCalculatorScale           = 18
)

// CalculatorTool 是通用高精度计算器，适合需要避免二进制浮点误差的场景。
//
// 实现使用 math/big.Rat 保存有理数，避免 float64/Java double 的二进制浮点误差。
// 例如 0.1 + 0.2 会得到精确的 0.3，而不是 0.30000000000000004。
type CalculatorTool struct{}

type calculatorInput struct {
	Expression   string `json:"expression"`
	Scale        *int   `json:"scale,omitempty"`
	RoundingMode string `json:"rounding_mode,omitempty"`
}

// NewCalculatorTool 创建计算器工具。
func NewCalculatorTool() tool.Tool {
	return &CalculatorTool{}
}

func (c *CalculatorTool) GetInfo() *tool.Info {
	return &tool.Info{
		Name:        "calculator",
		Description: "Agent内置高精度计算器。使用十进制/有理数精确算法，支持 +、-、*、/、括号、整数幂(^)、百分号(%)、sqrt()、abs()、round(value, scale)。显式传入scale时按scale和rounding_mode舍入输出。",
		Parameters: &tool.ParameterSchema{
			Type: "object",
			Properties: map[string]*tool.ParameterSchema{
				"expression": {
					Type:        "string",
					Description: "数学表达式，如: 0.1 + 0.2, 100 * (1 + 6.5%), sqrt(2), round(10 / 3, 2), 2^10",
				},
				"scale": {
					Type:        "integer",
					Description: "输出小数位数，范围0-18。省略时精确有限小数会完整输出，非整除和sqrt默认最多10位。",
				},
				"rounding_mode": {
					Type:        "string",
					Description: "舍入模式：half_up、half_even、down，默认half_up。",
					Enum:        []string{"half_up", "half_even", "down"},
				},
			},
			Required: []string{"expression"},
		},
	}
}

func (c *CalculatorTool) Execute(ctx context.Context, params string) (string, error) {
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("计算已取消: %w", ctx.Err())
	default:
	}

	var input calculatorInput
	if err := toolparam.Decode(params, &input); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}

	expr := strings.TrimSpace(input.Expression)
	if expr == "" {
		return "", fmt.Errorf("表达式不能为空")
	}
	if len([]rune(expr)) > maxCalculatorExpressionRunes {
		return "", fmt.Errorf("表达式过长，最多允许%d个字符", maxCalculatorExpressionRunes)
	}

	scale := defaultCalculatorScale
	scaleExplicit := input.Scale != nil
	if input.Scale != nil {
		if *input.Scale < 0 || *input.Scale > maxCalculatorScale {
			return "", fmt.Errorf("scale必须在0到%d之间", maxCalculatorScale)
		}
		scale = *input.Scale
	}

	roundingMode := strings.TrimSpace(strings.ToLower(input.RoundingMode))
	if roundingMode == "" {
		roundingMode = "half_up"
	}
	if !isSupportedRoundingMode(roundingMode) {
		return "", fmt.Errorf("不支持的舍入模式: %s", input.RoundingMode)
	}

	result, err := evaluateDecimal(ctx, expr, scale, roundingMode)
	if err != nil {
		return "", fmt.Errorf("计算失败: %w", err)
	}
	return formatRat(result, scale, roundingMode, scaleExplicit), nil
}

// decimalParser 是一个带资源限制的递归下降解析器。
type decimalParser struct {
	ctx          context.Context
	input        []rune
	pos          int
	depth        int
	scale        int
	roundingMode string
}

func evaluateDecimal(ctx context.Context, expr string, scale int, roundingMode string) (*big.Rat, error) {
	p := &decimalParser{ctx: ctx, input: []rune(expr), scale: scale, roundingMode: roundingMode}
	result, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	p.skipSpaces()
	if p.pos != len(p.input) {
		return nil, fmt.Errorf("表达式含有非法字符: %s", string(p.input[p.pos:]))
	}
	return result, nil
}

func (p *decimalParser) check() error {
	select {
	case <-p.ctx.Done():
		return fmt.Errorf("计算已取消: %w", p.ctx.Err())
	default:
	}
	if p.depth > maxCalculatorDepth {
		return fmt.Errorf("表达式嵌套过深，最多允许%d层", maxCalculatorDepth)
	}
	return nil
}

func (p *decimalParser) enter() error {
	p.depth++
	return p.check()
}

func (p *decimalParser) leave() {
	p.depth--
}

func (p *decimalParser) skipSpaces() {
	for p.pos < len(p.input) && unicode.IsSpace(p.input[p.pos]) {
		p.pos++
	}
}

func (p *decimalParser) parseExpr() (*big.Rat, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()

	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.input) || (p.input[p.pos] != '+' && p.input[p.pos] != '-') {
			return left, nil
		}
		op := p.input[p.pos]
		p.pos++
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		if op == '+' {
			left = new(big.Rat).Add(left, right)
		} else {
			left = new(big.Rat).Sub(left, right)
		}
	}
}

func (p *decimalParser) parseTerm() (*big.Rat, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.input) || (p.input[p.pos] != '*' && p.input[p.pos] != '/') {
			return left, nil
		}
		op := p.input[p.pos]
		p.pos++
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if op == '*' {
			left = new(big.Rat).Mul(left, right)
			continue
		}
		if right.Sign() == 0 {
			return nil, fmt.Errorf("除数不能为零")
		}
		left = new(big.Rat).Quo(left, right)
	}
}

func (p *decimalParser) parseUnary() (*big.Rat, error) {
	p.skipSpaces()
	if p.pos < len(p.input) && (p.input[p.pos] == '+' || p.input[p.pos] == '-') {
		op := p.input[p.pos]
		p.pos++
		value, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		if op == '-' {
			return new(big.Rat).Neg(value), nil
		}
		return value, nil
	}
	return p.parsePower()
}

func (p *decimalParser) parsePower() (*big.Rat, error) {
	base, err := p.parsePostfix()
	if err != nil {
		return nil, err
	}
	p.skipSpaces()
	if p.pos >= len(p.input) || p.input[p.pos] != '^' {
		return base, nil
	}
	p.pos++
	exponentValue, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	exponent, err := ratToBoundedInt(exponentValue)
	if err != nil {
		return nil, err
	}
	return powRat(base, exponent)
}

func (p *decimalParser) parsePostfix() (*big.Rat, error) {
	value, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != '%' {
			return value, nil
		}
		p.pos++
		value = new(big.Rat).Quo(value, big.NewRat(100, 1))
	}
}

func (p *decimalParser) parsePrimary() (*big.Rat, error) {
	p.skipSpaces()
	if p.pos >= len(p.input) {
		return nil, fmt.Errorf("表达式不完整")
	}

	ch := p.input[p.pos]
	if ch == '(' {
		p.pos++
		value, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("缺少右括号")
		}
		p.pos++
		return value, nil
	}
	if unicode.IsLetter(ch) {
		return p.parseFunction()
	}
	return p.parseNumber()
}

func (p *decimalParser) parseFunction() (*big.Rat, error) {
	start := p.pos
	for p.pos < len(p.input) && (unicode.IsLetter(p.input[p.pos]) || unicode.IsDigit(p.input[p.pos]) || p.input[p.pos] == '_') {
		p.pos++
	}
	funcName := strings.ToLower(string(p.input[start:p.pos]))

	p.skipSpaces()
	if p.pos >= len(p.input) || p.input[p.pos] != '(' {
		return nil, fmt.Errorf("函数 %s 缺少参数括号", funcName)
	}
	p.pos++

	arg, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	switch funcName {
	case "sqrt":
		if arg.Sign() < 0 {
			return nil, fmt.Errorf("sqrt的参数不能为负数")
		}
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("函数 %s 缺少右括号", funcName)
		}
		p.pos++
		return sqrtRat(arg, p.scale, p.roundingMode)
	case "abs":
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("函数 %s 缺少右括号", funcName)
		}
		p.pos++
		if arg.Sign() < 0 {
			return new(big.Rat).Neg(arg), nil
		}
		return arg, nil
	case "round":
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != ',' {
			return nil, fmt.Errorf("函数 round 需要两个参数: round(value, scale)")
		}
		p.pos++
		scaleRat, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		scale, err := ratToScale(scaleRat)
		if err != nil {
			return nil, err
		}
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("函数 %s 缺少右括号", funcName)
		}
		p.pos++
		return roundRat(arg, scale, "half_up"), nil
	default:
		return nil, fmt.Errorf("未知函数: %s", funcName)
	}
}

func (p *decimalParser) parseNumber() (*big.Rat, error) {
	p.skipSpaces()
	start := p.pos
	digitCount := 0
	dotSeen := false

	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if unicode.IsDigit(ch) {
			digitCount++
			if digitCount > maxCalculatorNumberDigits {
				return nil, fmt.Errorf("数字过长，最多允许%d位数字", maxCalculatorNumberDigits)
			}
			p.pos++
			continue
		}
		if ch == '.' && !dotSeen {
			dotSeen = true
			p.pos++
			continue
		}
		break
	}

	if start == p.pos || digitCount == 0 {
		if p.pos >= len(p.input) {
			return nil, fmt.Errorf("预期数字，但表达式已结束")
		}
		return nil, fmt.Errorf("预期数字，但遇到: %s", string(p.input[p.pos]))
	}

	numberText := string(p.input[start:p.pos])
	value, ok := new(big.Rat).SetString(numberText)
	if !ok {
		return nil, fmt.Errorf("非法数字: %s", numberText)
	}
	return value, nil
}

func ratToBoundedInt(value *big.Rat) (int64, error) {
	if value.Denom().Cmp(big.NewInt(1)) != 0 {
		return 0, fmt.Errorf("幂指数必须是整数")
	}
	exponent := value.Num()
	limit := big.NewInt(maxCalculatorExponentAbs)
	if new(big.Int).Abs(exponent).Cmp(limit) > 0 {
		return 0, fmt.Errorf("幂指数绝对值不能超过%d", maxCalculatorExponentAbs)
	}
	return exponent.Int64(), nil
}

func ratToScale(value *big.Rat) (int, error) {
	if value.Denom().Cmp(big.NewInt(1)) != 0 {
		return 0, fmt.Errorf("scale必须是整数")
	}
	if value.Num().Sign() < 0 || value.Num().Cmp(big.NewInt(maxCalculatorScale)) > 0 {
		return 0, fmt.Errorf("scale必须在0到%d之间", maxCalculatorScale)
	}
	return int(value.Num().Int64()), nil
}

func powRat(base *big.Rat, exponent int64) (*big.Rat, error) {
	if exponent == 0 {
		return big.NewRat(1, 1), nil
	}
	if base.Sign() == 0 && exponent < 0 {
		return nil, fmt.Errorf("零不能作为负指数幂的底数")
	}
	negative := exponent < 0
	if negative {
		exponent = -exponent
	}

	result := big.NewRat(1, 1)
	factor := new(big.Rat).Set(base)
	for exponent > 0 {
		if exponent%2 == 1 {
			result.Mul(result, factor)
		}
		exponent /= 2
		if exponent > 0 {
			factor.Mul(factor, factor)
		}
	}
	if negative {
		if result.Sign() == 0 {
			return nil, fmt.Errorf("零不能作为负指数幂的底数")
		}
		result.Inv(result)
	}
	return result, nil
}

func sqrtRat(value *big.Rat, scale int, roundingMode string) (*big.Rat, error) {
	if value.Sign() == 0 {
		return big.NewRat(0, 1), nil
	}

	// sqrt 通常会产生无理数，先用高精度 big.Float 近似，再统一走十进制舍入。
	precision := uint((scale + 16) * 4)
	if precision < 256 {
		precision = 256
	}
	floatValue := new(big.Float).SetPrec(precision).SetMode(big.ToNearestEven).SetRat(value)
	sqrtValue := new(big.Float).SetPrec(precision).SetMode(big.ToNearestEven).Sqrt(floatValue)
	decimalText := sqrtValue.Text('f', scale+8)
	approx, ok := new(big.Rat).SetString(decimalText)
	if !ok {
		return nil, fmt.Errorf("sqrt结果转换失败")
	}
	return roundRat(approx, scale, roundingMode), nil
}

func formatRat(value *big.Rat, scale int, roundingMode string, scaleExplicit bool) string {
	if scaleExplicit {
		return trimTrailingZeros(roundRat(value, scale, roundingMode).FloatString(scale))
	}
	if terminatesInBase10(value) {
		return trimTrailingZeros(value.FloatString(decimalPlacesForTerminating(value)))
	}
	return trimTrailingZeros(roundRat(value, scale, roundingMode).FloatString(scale))
}

func roundRat(value *big.Rat, scale int, roundingMode string) *big.Rat {
	factor := pow10(scale)
	scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt(factor))
	quotient := new(big.Int).Quo(scaled.Num(), scaled.Denom())
	remainder := new(big.Int).Rem(scaled.Num(), scaled.Denom())
	if remainder.Sign() < 0 {
		remainder.Abs(remainder)
	}

	increment := shouldIncrement(value.Sign(), quotient, remainder, scaled.Denom(), roundingMode)
	if increment != 0 {
		quotient.Add(quotient, big.NewInt(increment))
	}
	return new(big.Rat).SetFrac(quotient, factor)
}

func shouldIncrement(sign int, quotient, remainder, denominator *big.Int, roundingMode string) int64 {
	if remainder.Sign() == 0 || roundingMode == "down" {
		return 0
	}
	direction := int64(1)
	if sign < 0 {
		direction = -1
	}
	twiceRemainder := new(big.Int).Mul(remainder, big.NewInt(2))
	cmp := twiceRemainder.Cmp(denominator)
	if cmp > 0 {
		return direction
	}
	if cmp < 0 {
		return 0
	}
	if roundingMode == "half_up" {
		return direction
	}
	if roundingMode == "half_even" && new(big.Int).Abs(quotient).Bit(0) == 1 {
		return direction
	}
	return 0
}

func terminatesInBase10(value *big.Rat) bool {
	den := new(big.Int).Abs(value.Denom())
	removeFactor(den, 2)
	removeFactor(den, 5)
	return den.Cmp(big.NewInt(1)) == 0
}

func decimalPlacesForTerminating(value *big.Rat) int {
	den := new(big.Int).Abs(value.Denom())
	twos := removeFactor(den, 2)
	fives := removeFactor(den, 5)
	if twos > fives {
		return twos
	}
	return fives
}

func removeFactor(value *big.Int, factor int64) int {
	count := 0
	bigFactor := big.NewInt(factor)
	zero := big.NewInt(0)
	mod := new(big.Int)
	for value.Cmp(big.NewInt(1)) > 0 {
		mod.Mod(value, bigFactor)
		if mod.Cmp(zero) != 0 {
			break
		}
		value.Quo(value, bigFactor)
		count++
	}
	return count
}

func pow10(scale int) *big.Int {
	return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
}

func trimTrailingZeros(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	value = strings.TrimRight(value, "0")
	value = strings.TrimRight(value, ".")
	if value == "-0" {
		return "0"
	}
	return value
}

func isSupportedRoundingMode(mode string) bool {
	switch mode {
	case "half_up", "half_even", "down":
		return true
	default:
		return false
	}
}
