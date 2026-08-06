package query

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type relationSource struct {
	Key *int64
}

type relationTarget struct {
	Key int64
}

func TestRelationConstructorsRejectInvalidConfiguration(t *testing.T) {
	t.Parallel()

	requiredSource := RequiredKey(func(value relationSource) int64 { return *value.Key })
	requiredTarget := RequiredKey(func(value relationTarget) int64 { return value.Key })
	queryFactory := func([]int64) SelectBuilder[relationTarget] { return SelectBuilder[relationTarget]{} }

	tests := []struct {
		name string
		make func() error
	}{
		{"missing name", func() error {
			_, err := NewManyRelation("", requiredSource, requiredTarget, queryFactory)
			return err
		}},
		{"nil source key", func() error {
			_, err := NewManyRelation[relationSource, relationTarget, int64]("items", nil, requiredTarget, queryFactory)
			return err
		}},
		{"nil target key", func() error {
			_, err := NewOneRelation[relationSource, relationTarget, int64]("item", requiredSource, nil, queryFactory)
			return err
		}},
		{"nil query", func() error {
			_, err := NewOneRelation[relationSource, relationTarget, int64]("item", requiredSource, requiredTarget, nil)
			return err
		}},
		{"nil option", func() error {
			_, err := NewManyRelation("items", requiredSource, requiredTarget, queryFactory, nil)
			return err
		}},
		{"zero chunk", func() error {
			_, err := NewManyRelation("items", requiredSource, requiredTarget, queryFactory, WithRelationChunkSize(0))
			return err
		}},
		{"oversized chunk", func() error {
			_, err := NewManyRelation("items", requiredSource, requiredTarget, queryFactory, WithRelationChunkSize(65536))
			return err
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.make(); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestManyRelationEmptyAndAllNullSourcesExecuteNoQuery(t *testing.T) {
	t.Parallel()

	queryCalls := 0
	relation, err := NewManyRelation(
		"targets",
		func(value relationSource) (int64, bool) {
			if value.Key == nil {
				return 0, false
			}
			return *value.Key, true
		},
		RequiredKey(func(value relationTarget) int64 { return value.Key }),
		func([]int64) SelectBuilder[relationTarget] {
			queryCalls++
			return SelectBuilder[relationTarget]{}
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	empty, err := relation.Load(context.Background(), nil, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty load result=%#v err=%v", empty, err)
	}
	nullResults, err := relation.Load(context.Background(), nil, []relationSource{{}, {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(nullResults) != 2 || nullResults[0].KeyPresent || nullResults[1].KeyPresent {
		t.Fatalf("unexpected null results: %#v", nullResults)
	}
	if queryCalls != 0 {
		t.Fatalf("query factory calls = %d, want 0", queryCalls)
	}
}

func TestRelationContainsCallbackPanicsAndRejectsPagination(t *testing.T) {
	t.Parallel()

	key := int64(1)
	sources := []relationSource{{Key: &key}}
	targetKey := RequiredKey(func(value relationTarget) int64 { return value.Key })

	queryPanic, err := NewManyRelation(
		"secret-relation",
		RequiredKey(func(relationSource) int64 { return key }),
		targetKey,
		func([]int64) SelectBuilder[relationTarget] { panic("secret-key-value") },
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = queryPanic.Load(context.Background(), nil, sources)
	if err == nil {
		t.Fatal("expected query factory panic error")
	}
	if strings.Contains(err.Error(), "secret-key-value") {
		t.Fatalf("relation error leaked panic value: %v", err)
	}
	var relationErr *RelationError
	if !errors.As(err, &relationErr) || relationErr.Chunk != 1 {
		t.Fatalf("unexpected relation error: %#v", err)
	}

	keyPanic, err := NewManyRelation(
		"targets",
		func(relationSource) (int64, bool) { panic("source-secret") },
		targetKey,
		func([]int64) SelectBuilder[relationTarget] { return SelectBuilder[relationTarget]{} },
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = keyPanic.Load(context.Background(), nil, sources)
	if err == nil || strings.Contains(err.Error(), "source-secret") {
		t.Fatalf("source extractor error = %v", err)
	}

	table, tableErr := NewTable[relationTarget]("targets", []string{"key"}, func(row RowScanner) (relationTarget, error) {
		var value relationTarget
		err := row.Scan(&value.Key)
		return value, err
	})
	if tableErr != nil {
		t.Fatal(tableErr)
	}
	column, columnErr := NewColumn[relationTarget, int64](table, "key")
	if columnErr != nil {
		t.Fatal(columnErr)
	}
	paginated, err := NewManyRelation(
		"targets",
		RequiredKey(func(relationSource) int64 { return key }),
		targetKey,
		SelectRelationByColumn(Select(table).Limit(1), column),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = paginated.Load(context.Background(), nil, sources)
	if err == nil {
		t.Fatal("expected pagination rejection")
	}
}

func TestRelationErrorDoesNotExposeUnderlyingKeyText(t *testing.T) {
	t.Parallel()

	cause := errors.New("driver mentioned key tenant-secret")
	err := relationFailure("orders", 2, cause)
	if strings.Contains(err.Error(), "tenant-secret") {
		t.Fatalf("public relation error leaked cause: %v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatal("relation error does not preserve cause")
	}
}
