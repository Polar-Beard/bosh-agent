package monit

import (
	"net/http"
	"strings"
	"time"

	"code.cloudfoundry.org/clock"
	boshretry "github.com/cloudfoundry/bosh-utils/retrystrategy"
)

//go:generate counterfeiter . RequestRetryable

type RequestRetryable interface {
	Attempt() (bool, error)
	Response() *http.Response
}

type monitRetryStrategy struct {
	retryable RequestRetryable

	maxUnavailableAttempts uint
	maxOtherAttempts       uint

	delay       time.Duration
	timeService clock.Clock

	unavailableAttempts uint
	otherAttempts       uint
}

func NewMonitRetryStrategy(
	retryable RequestRetryable,
	maxUnavailableAttempts uint,
	maxOtherAttempts uint,
	delay time.Duration,
	timeService clock.Clock,
) boshretry.RetryStrategy {
	return &monitRetryStrategy{
		retryable:              retryable,
		maxUnavailableAttempts: maxUnavailableAttempts,
		maxOtherAttempts:       maxOtherAttempts,
		unavailableAttempts:    0,
		otherAttempts:          0,
		delay:                  delay,
		timeService:            timeService,
	}
}

func (m *monitRetryStrategy) Try() error {
	var err error
	var shouldRetry bool

	for m.hasMoreAttempts() {
		shouldRetry, err = m.retryable.Attempt()
		if !shouldRetry {
			break
		}

		is503 := m.retryable.Response() != nil && m.retryable.Response().StatusCode == 503
		isCanceled := err != nil && strings.Contains(err.Error(), "request canceled")

		if (is503 || isCanceled) && m.unavailableAttempts < m.maxUnavailableAttempts {
			m.unavailableAttempts = m.unavailableAttempts + 1
		} else {
			// once a non-503 error is received, all errors count as 'other' errors
			m.unavailableAttempts = m.maxUnavailableAttempts + 1
			m.otherAttempts = m.otherAttempts + 1
		}

		m.timeService.Sleep(m.delay)
	}

	if err != nil && m.retryable.Response() != nil {
		_ = m.retryable.Response().Body.Close()
	}

	return err
}

func (m *monitRetryStrategy) hasMoreAttempts() bool {
	return m.unavailableAttempts < m.maxUnavailableAttempts || m.otherAttempts < m.maxOtherAttempts
}
