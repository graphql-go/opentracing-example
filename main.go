package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/gqlerrors"
	"github.com/graphql-go/graphql/testutil"
	"github.com/graphql-go/handler"

	"sourcegraph.com/sourcegraph/appdash"
	appdashtracer "sourcegraph.com/sourcegraph/appdash/opentracing"
	"sourcegraph.com/sourcegraph/appdash/traceapp"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
)

func main() {
	startAppdashServer()
	h := handler.New(&handler.Config{
		Schema:   &testutil.StarWarsSchema,
		Pretty:   true,
		GraphiQL: true,
		Tracer: OpenTracingTracer{},
	})
	http.Handle("/graphql", h)
	log.Printf("server running on :8080")
	http.ListenAndServe(":8080", nil)
}

func startAppdashServer() opentracing.Tracer {
	memStore := appdash.NewMemoryStore()
	store := &appdash.RecentStore{
		MinEvictAge: 5 * time.Minute,
		DeleteStore: memStore,
	}

	url, err := url.Parse("http://localhost:8700")
	if err != nil {
		log.Fatal(err)
	}
	tapp, err := traceapp.New(nil, url)
	if err != nil {
		log.Fatal(err)
	}
	tapp.Store = store
	tapp.Queryer = memStore

	go func() {
		log.Fatal(http.ListenAndServe(":8700", tapp))
	}()
	tapp.Store = store
	tapp.Queryer = memStore

	collector := appdash.NewLocalCollector(store)

	tracer := appdashtracer.NewTracer(collector)

	opentracing.SetGlobalTracer(tracer)

	log.Println("Appdash web UI running on HTTP :8700")
	return tracer
}

type OpenTracingTracer struct{}

func (OpenTracingTracer) TraceQuery(ctx context.Context, queryString string, operationName string) (context.Context, graphql.TraceQueryFinishFunc) {
	span, spanCtx := opentracing.StartSpanFromContext(ctx, "GraphQL request")
	log.Printf("\n\n TraceQuery, span: %+v \n\n", span)
	span.SetTag("graphql.query", queryString)

	if operationName != "" {
		span.SetTag("graphql.operationName", operationName)
	}

	return spanCtx, func(errs []gqlerrors.FormattedError) {
		if len(errs) > 0 {
			msg := errs[0].Error()
			if len(errs) > 1 {
				msg += fmt.Sprintf(" (and %d more errors)", len(errs)-1)
			}
			ext.Error.Set(span, true)
			span.SetTag("graphql.error", msg)
		}
		log.Printf("\n\n TraceQuery finish called, span: %+v \n\n", span)
		span.Finish()
	}
}

func (OpenTracingTracer) TraceField(ctx context.Context, fieldName string) graphql.TraceFieldFinishFunc {
	// label := fmt.Sprintf("GraphQL field: %s.%s", typeName, fieldName)
	label := fmt.Sprintf("GraphQL field: TypeName.%s", fieldName)
	span, _ := opentracing.StartSpanFromContext(ctx, label)
	log.Printf("\n\n TraceField, fieldName: %s, span: %+v \n\n", fieldName, span)
	span.SetTag("graphql.field", fieldName)
	return func(err gqlerrors.FormattedError) {
		if err.OriginalError() != nil {
			ext.Error.Set(span, true)
			span.SetTag("graphql.error", err.Error())
		}
		log.Printf("\n\n TraceField finish called, span: %+v \n\n", span)
		span.Finish()
	}
}
