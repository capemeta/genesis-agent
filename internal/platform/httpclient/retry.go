package httpclient

import "time"

// RetryPolicy 描述请求重试策略。
type RetryPolicy struct {
	MaxAttempts      int
	InitialBackoff   time.Duration
	MaxBackoff       time.Duration
	Multiplier       float64
	Jitter           bool
	RetryStatusCodes []int
	RetryMethods     []string
}

func (p RetryPolicy) normalized() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 1
	}
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = 200 * time.Millisecond
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = 2 * time.Second
	}
	if p.Multiplier <= 0 {
		p.Multiplier = 2
	}
	if len(p.RetryStatusCodes) == 0 {
		p.RetryStatusCodes = []int{
			429,
			502,
			503,
			504,
		}
	}
	if len(p.RetryMethods) == 0 {
		p.RetryMethods = []string{
			"GET",
			"HEAD",
			"PUT",
			"DELETE",
		}
	}
	return p
}
