// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package jaeger_test

import (
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"go.opentelemetry.io/contrib/propagators/jaeger"
)

func TestExtractJaeger(t *testing.T) {
	testGroup := []struct {
		name      string
		testcases []extractTest
	}{
		{
			name:      "valid test case",
			testcases: extractHeaders,
		},
		{
			name:      "invalid test case",
			testcases: invalidExtractHeaders,
		},
	}

	for _, tg := range testGroup {
		propagator := jaeger.Jaeger{}

		for _, tc := range tg.testcases {
			t.Run(tc.name, func(t *testing.T) {
				header := make(http.Header, len(tc.headers))
				for k, v := range tc.headers {
					header.Set(k, v)
				}

				ctx := t.Context()
				ctx = propagator.Extract(ctx, propagation.HeaderCarrier(header))
				resSc := trace.SpanContextFromContext(ctx)
				comparer := cmp.Comparer(func(a, b trace.SpanContext) bool {
					// Do not compare remote field, it is unset on empty
					// SpanContext.
					newA := a.WithRemote(b.IsRemote())
					return newA.Equal(b)
				})
				if diff := cmp.Diff(resSc, trace.NewSpanContext(tc.expected), comparer); diff != "" {
					t.Errorf("%s: %s: -got +want %s", tg.name, tc.name, diff)
				}
				assert.Equal(t, tc.debug, jaeger.DebugFromContext(ctx))
			})
		}
	}
}

func TestInjectJaeger(t *testing.T) {
	testGroup := []struct {
		name      string
		testcases []injectTest
	}{
		{
			name:      "valid test case",
			testcases: injectHeaders,
		},
		{
			name:      "invalid test case",
			testcases: invalidInjectHeaders,
		},
	}

	for _, tg := range testGroup {
		for _, tc := range tg.testcases {
			propagator := jaeger.Jaeger{}
			t.Run(tc.name, func(t *testing.T) {
				header := http.Header{}
				ctx := trace.ContextWithSpanContext(
					jaeger.WithDebug(t.Context(), tc.debug),
					trace.NewSpanContext(tc.scc),
				)
				propagator.Inject(ctx, propagation.HeaderCarrier(header))

				for h, v := range tc.wantHeaders {
					result, want := header.Get(h), v
					if diff := cmp.Diff(result, want); diff != "" {
						t.Errorf("%s: %s, header=%s: -got +want %s", tg.name, tc.name, h, diff)
					}
				}
			})
		}
	}
}

func TestExtractJaegerReplacesDebugFlag(t *testing.T) {
	propagator := jaeger.Jaeger{}
	for _, previousFlags := range []string{"1", "3"} {
		for _, flags := range []string{"", "0", "1", "2", "3", "8", "9"} {
			t.Run(previousFlags+"/"+flags, func(t *testing.T) {
				ctx := propagator.Extract(t.Context(), propagation.MapCarrier{
					"uber-trace-id": "11111111111111111111111111111111:1111111111111111:0:" + previousFlags,
				})
				header := "22222222222222222222222222222222:2222222222222222:0:"
				ctx = propagator.Extract(ctx, propagation.MapCarrier{"uber-trace-id": header + flags})
				sc := trace.SpanContextFromContext(ctx)
				require.True(t, sc.IsValid())
				assert.True(t, sc.IsRemote())
				assert.Equal(t, "22222222222222222222222222222222", sc.TraceID().String())
				assert.Equal(t, "2222222222222222", sc.SpanID().String())
				assert.Equal(t, flags == "3", jaeger.DebugFromContext(ctx))
				expectedFlags := "0"
				switch flags {
				case "1", "9":
					expectedFlags = "1"
				case "3":
					expectedFlags = "3"
				}
				carrier := propagation.MapCarrier{}
				propagator.Inject(ctx, carrier)
				assert.Equal(t, header+expectedFlags, carrier["uber-trace-id"])
			})
		}
	}
}

func TestExtractJaegerInvalidHeaderPreservesContext(t *testing.T) {
	propagator := jaeger.Jaeger{}
	ctx := propagator.Extract(t.Context(), propagation.MapCarrier{
		"uber-trace-id": "11111111111111111111111111111111:1111111111111111:0:3",
	})
	for _, header := range []string{"", "invalid", "00000000000000000000000000000000:2222222222222222:0:0", "22222222222222222222222222222222::0:0"} {
		t.Run(header, func(t *testing.T) {
			got := propagator.Extract(ctx, propagation.MapCarrier{"uber-trace-id": header})
			assert.Equal(t, ctx, got)
			assert.True(t, jaeger.DebugFromContext(got))
		})
	}
}

func BenchmarkJaegerExtract(b *testing.B) {
	propagator := jaeger.Jaeger{}
	for _, previousFlags := range []string{"1", "3"} {
		ctx := propagator.Extract(b.Context(), propagation.MapCarrier{
			"uber-trace-id": "11111111111111111111111111111111:1111111111111111:0:" + previousFlags,
		})
		for _, flags := range []string{"0", "1", "3"} {
			carrier := propagation.MapCarrier{
				"uber-trace-id": "22222222222222222222222222222222:2222222222222222:0:" + flags,
			}
			b.Run(previousFlags+"/"+flags, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					propagator.Extract(ctx, carrier)
				}
			})
		}
	}
}
