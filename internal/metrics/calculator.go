package metrics

import (
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/nodestage/sessionhub/internal/domain"
)

type Usage struct {
	InputTokens  *int64
	OutputTokens *int64
	CacheRead    *int64
	CacheWrite   *int64
}

type Calculator struct {
	tokenizers map[string]func(string) int64
}

func NewCalculator() *Calculator {
	words := regexp.MustCompile(`[\pL\pN_]+|[^\s\pL\pN_]`)
	return &Calculator{tokenizers: map[string]func(string) int64{
		"unicode_words": func(value string) int64 { return int64(len(words.FindAllString(value, -1))) },
		"bytes_4": func(value string) int64 {
			n := int64(len([]byte(value)))
			return (n + 3) / 4
		},
	}}
}

func (c *Calculator) Register(name string, tokenizer func(string) int64) error {
	if name == "" || tokenizer == nil {
		return fmt.Errorf("tokenizer name and function are required")
	}
	c.tokenizers[name] = tokenizer
	return nil
}

func (c *Calculator) Measure(
	input, output, tokenizer string,
	reported Usage,
) domain.Metric {
	metric := domain.Metric{Precision: domain.PrecisionExact}
	allReported := reported.InputTokens != nil && reported.OutputTokens != nil
	if reported.InputTokens != nil {
		metric.InputTokens = *reported.InputTokens
	}
	if reported.OutputTokens != nil {
		metric.OutputTokens = *reported.OutputTokens
	}
	if reported.CacheRead != nil {
		metric.CacheRead = *reported.CacheRead
	}
	if reported.CacheWrite != nil {
		metric.CacheWrite = *reported.CacheWrite
	}
	if allReported {
		return metric
	}
	if tokenize, ok := c.tokenizers[tokenizer]; ok {
		if reported.InputTokens == nil {
			metric.InputTokens = tokenize(input)
		}
		if reported.OutputTokens == nil {
			metric.OutputTokens = tokenize(output)
		}
		metric.Precision = domain.PrecisionEstimated
		return metric
	}
	if reported.InputTokens == nil {
		metric.InputTokens = approximate(input)
	}
	if reported.OutputTokens == nil {
		metric.OutputTokens = approximate(output)
	}
	metric.Precision = domain.PrecisionApproximate
	return metric
}

func approximate(value string) int64 {
	runes := int64(utf8.RuneCountInString(value))
	if runes == 0 {
		return 0
	}
	return (runes + 3) / 4
}

func ApplyPrice(metric domain.Metric, price domain.Price) domain.Metric {
	if price.ZeroCostExplicit {
		metric.CostMicrosUSD = 0
		metric.PriceVersion = price.Version
		return metric
	}
	const million = int64(1_000_000)
	metric.CostMicrosUSD =
		metric.InputTokens*price.InputMicros/million +
			metric.OutputTokens*price.OutputMicros/million +
			metric.CacheRead*price.CacheReadMicros/million +
			metric.CacheWrite*price.CacheWriteMicros/million
	metric.PriceVersion = price.Version
	return metric
}
