package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const defaultRelationChunkSize = 1000

// ErrRelationMultipleRows indicates that a one-relation query returned more
// than one target row for the same key.
var ErrRelationMultipleRows = errors.New("query: one relation returned multiple target rows")

// ErrRelationUnexpectedKey indicates that a relation query returned a target
// row whose extracted key was not requested in that chunk.
var ErrRelationUnexpectedKey = errors.New("query: relation query returned an unexpected target key")

// RelationError adds relation and chunk context without formatting key values.
type RelationError struct {
	Relation string
	Chunk    int
	Err      error
}

func (e *RelationError) Error() string {
	if e == nil {
		return "query: relation load failed"
	}
	if e.Chunk > 0 {
		return fmt.Sprintf("query: relation %q chunk %d failed", e.Relation, e.Chunk)
	}
	return fmt.Sprintf("query: relation %q failed", e.Relation)
}

func (e *RelationError) Unwrap() error { return e.Err }

// KeyFunc extracts a comparable relation key. present=false represents a NULL
// or otherwise absent relation key.
type KeyFunc[T any, K comparable] func(T) (key K, present bool)

// RequiredKey adapts a required key extractor into a KeyFunc.
func RequiredKey[T any, K comparable](extract func(T) K) KeyFunc[T, K] {
	if extract == nil {
		return nil
	}
	return func(value T) (K, bool) { return extract(value), true }
}

// RelationQuery builds one typed SELECT for a non-empty chunk of unique keys.
type RelationQuery[T any, K comparable] func(keys []K) SelectBuilder[T]

// SelectRelationByColumn creates a single-column relation query. The base
// builder remains immutable and may include additional filters and ordering.
func SelectRelationByColumn[T any, K comparable](base SelectBuilder[T], column Column[T, K]) RelationQuery[T, K] {
	return func(keys []K) SelectBuilder[T] {
		return base.Where(column.In(keys...))
	}
}

// RelationOption configures a one or many relation loader.
type RelationOption func(*relationConfig) error

type relationConfig struct {
	chunkSize int
}

// WithRelationChunkSize sets the maximum unique keys passed to each relation
// query. The caller must keep the resulting PostgreSQL parameter count within
// the database limit when a composite-key query uses multiple parameters per key.
func WithRelationChunkSize(size int) RelationOption {
	return func(config *relationConfig) error {
		if size <= 0 {
			return fmt.Errorf("query: relation chunk size must be positive")
		}
		if size > 65535 {
			return fmt.Errorf("query: relation chunk size cannot exceed 65535")
		}
		config.chunkSize = size
		return nil
	}
}

// ManyResult is aligned with one source row. KeyPresent distinguishes a NULL
// source key from a present key that has no related target rows.
type ManyResult[T any] struct {
	Values     []T
	KeyPresent bool
}

// OneResult is aligned with one source row. KeyPresent distinguishes a NULL
// source key; Found distinguishes a missing target row from a loaded target.
type OneResult[T any] struct {
	Value      T
	KeyPresent bool
	Found      bool
}

// ManyRelation explicitly loads zero or more target rows for each source row.
type ManyRelation[S any, T any, K comparable] struct {
	name       string
	sourceKey  KeyFunc[S, K]
	targetKey  KeyFunc[T, K]
	query      RelationQuery[T, K]
	chunkSize  int
	configured bool
}

// NewManyRelation validates and creates an immutable many-relation loader.
func NewManyRelation[S any, T any, K comparable](
	name string,
	sourceKey KeyFunc[S, K],
	targetKey KeyFunc[T, K],
	query RelationQuery[T, K],
	options ...RelationOption,
) (ManyRelation[S, T, K], error) {
	config, err := newRelationConfig(name, sourceKey != nil, targetKey != nil, query != nil, options)
	if err != nil {
		return ManyRelation[S, T, K]{}, err
	}
	return ManyRelation[S, T, K]{
		name:       strings.TrimSpace(name),
		sourceKey:  sourceKey,
		targetKey:  targetKey,
		query:      query,
		chunkSize:  config.chunkSize,
		configured: true,
	}, nil
}

// Load executes no query for an empty source list or when every source key is
// absent. Otherwise it executes exactly one query per unique-key chunk.
func (relation ManyRelation[S, T, K]) Load(ctx context.Context, executor *Executor, sources []S) ([]ManyResult[T], error) {
	if !relation.configured {
		return nil, fmt.Errorf("query: many relation is not configured")
	}
	keys, positions, results, err := collectSourceKeys[S, T, K](relation.name, sources, relation.sourceKey)
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return results, nil
	}

	grouped := make(map[K][]T, len(keys))
	for chunkIndex, start := 0, 0; start < len(keys); chunkIndex, start = chunkIndex+1, start+relation.chunkSize {
		if err := ctx.Err(); err != nil {
			return nil, relationFailure(relation.name, chunkIndex+1, err)
		}
		end := min(start+relation.chunkSize, len(keys))
		chunk := append([]K(nil), keys[start:end]...)
		builder, err := buildRelationQuery(relation.name, chunkIndex+1, relation.query, chunk)
		if err != nil {
			return nil, err
		}
		targets, err := FetchAll(ctx, executor, builder)
		if err != nil {
			return nil, relationFailure(relation.name, chunkIndex+1, err)
		}
		requested := make(map[K]struct{}, len(chunk))
		for _, key := range chunk {
			requested[key] = struct{}{}
		}
		for _, target := range targets {
			key, present, err := extractRelationKey(relation.name, "target", relation.targetKey, target)
			if err != nil {
				return nil, relationFailure(relation.name, chunkIndex+1, err)
			}
			if !present {
				continue
			}
			if _, exists := requested[key]; !exists {
				return nil, relationFailure(relation.name, chunkIndex+1, ErrRelationUnexpectedKey)
			}
			grouped[key] = append(grouped[key], target)
		}
	}

	for key, indexes := range positions {
		values := grouped[key]
		for _, index := range indexes {
			results[index].Values = cloneRelationValues(values)
		}
	}
	return results, nil
}

// OneRelation explicitly loads at most one target row for each source row.
type OneRelation[S any, T any, K comparable] struct {
	name       string
	sourceKey  KeyFunc[S, K]
	targetKey  KeyFunc[T, K]
	query      RelationQuery[T, K]
	chunkSize  int
	configured bool
}

// NewOneRelation validates and creates an immutable one-relation loader.
func NewOneRelation[S any, T any, K comparable](
	name string,
	sourceKey KeyFunc[S, K],
	targetKey KeyFunc[T, K],
	query RelationQuery[T, K],
	options ...RelationOption,
) (OneRelation[S, T, K], error) {
	config, err := newRelationConfig(name, sourceKey != nil, targetKey != nil, query != nil, options)
	if err != nil {
		return OneRelation[S, T, K]{}, err
	}
	return OneRelation[S, T, K]{
		name:       strings.TrimSpace(name),
		sourceKey:  sourceKey,
		targetKey:  targetKey,
		query:      query,
		chunkSize:  config.chunkSize,
		configured: true,
	}, nil
}

// Load executes no query for an empty source list or when every source key is
// absent. It rejects duplicate target rows for one key.
func (relation OneRelation[S, T, K]) Load(ctx context.Context, executor *Executor, sources []S) ([]OneResult[T], error) {
	if !relation.configured {
		return nil, fmt.Errorf("query: one relation is not configured")
	}
	keys, positions, manyResults, err := collectSourceKeys[S, T, K](relation.name, sources, relation.sourceKey)
	if err != nil {
		return nil, err
	}
	results := make([]OneResult[T], len(sources))
	for index := range manyResults {
		results[index].KeyPresent = manyResults[index].KeyPresent
	}
	if len(keys) == 0 {
		return results, nil
	}

	loaded := make(map[K]T, len(keys))
	found := make(map[K]bool, len(keys))
	for chunkIndex, start := 0, 0; start < len(keys); chunkIndex, start = chunkIndex+1, start+relation.chunkSize {
		if err := ctx.Err(); err != nil {
			return nil, relationFailure(relation.name, chunkIndex+1, err)
		}
		end := min(start+relation.chunkSize, len(keys))
		chunk := append([]K(nil), keys[start:end]...)
		builder, err := buildRelationQuery(relation.name, chunkIndex+1, relation.query, chunk)
		if err != nil {
			return nil, err
		}
		targets, err := FetchAll(ctx, executor, builder)
		if err != nil {
			return nil, relationFailure(relation.name, chunkIndex+1, err)
		}
		requested := make(map[K]struct{}, len(chunk))
		for _, key := range chunk {
			requested[key] = struct{}{}
		}
		for _, target := range targets {
			key, present, err := extractRelationKey(relation.name, "target", relation.targetKey, target)
			if err != nil {
				return nil, relationFailure(relation.name, chunkIndex+1, err)
			}
			if !present {
				continue
			}
			if _, exists := requested[key]; !exists {
				return nil, relationFailure(relation.name, chunkIndex+1, ErrRelationUnexpectedKey)
			}
			if found[key] {
				return nil, relationFailure(relation.name, chunkIndex+1, ErrRelationMultipleRows)
			}
			loaded[key], found[key] = target, true
		}
	}

	for key, indexes := range positions {
		value, exists := loaded[key]
		for _, index := range indexes {
			results[index].Found = exists
			if exists {
				results[index].Value = value
			}
		}
	}
	return results, nil
}

func newRelationConfig(name string, sourceKey, targetKey, query bool, options []RelationOption) (relationConfig, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return relationConfig{}, fmt.Errorf("query: relation name is required")
	}
	if strings.ContainsRune(trimmed, '\x00') {
		return relationConfig{}, fmt.Errorf("query: relation name contains NUL")
	}
	if !sourceKey {
		return relationConfig{}, fmt.Errorf("query: relation %q source key extractor is nil", trimmed)
	}
	if !targetKey {
		return relationConfig{}, fmt.Errorf("query: relation %q target key extractor is nil", trimmed)
	}
	if !query {
		return relationConfig{}, fmt.Errorf("query: relation %q query factory is nil", trimmed)
	}
	config := relationConfig{chunkSize: defaultRelationChunkSize}
	for index, option := range options {
		if option == nil {
			return relationConfig{}, fmt.Errorf("query: relation option %d is nil", index)
		}
		if err := option(&config); err != nil {
			return relationConfig{}, err
		}
	}
	return config, nil
}

func collectSourceKeys[S any, T any, K comparable](
	name string,
	sources []S,
	extract KeyFunc[S, K],
) ([]K, map[K][]int, []ManyResult[T], error) {
	positions := make(map[K][]int)
	keys := make([]K, 0, len(sources))
	results := make([]ManyResult[T], len(sources))
	for index, source := range sources {
		key, present, err := extractRelationKey(name, "source", extract, source)
		if err != nil {
			return nil, nil, nil, relationFailure(name, 0, err)
		}
		if !present {
			continue
		}
		results[index].KeyPresent = true
		results[index].Values = make([]T, 0)
		if _, exists := positions[key]; !exists {
			keys = append(keys, key)
		}
		positions[key] = append(positions[key], index)
	}
	return keys, positions, results, nil
}

func buildRelationQuery[T any, K comparable](name string, chunk int, factory RelationQuery[T, K], keys []K) (builder SelectBuilder[T], err error) {
	defer func() {
		if recover() != nil {
			builder = SelectBuilder[T]{}
			err = relationFailure(name, chunk, fmt.Errorf("query: relation query factory panicked"))
		}
	}()
	builder = factory(append([]K(nil), keys...))
	if builder.limit != nil || builder.offset != nil {
		return SelectBuilder[T]{}, relationFailure(name, chunk, fmt.Errorf("query: relation query cannot use LIMIT or OFFSET"))
	}
	return builder, nil
}

func extractRelationKey[T any, K comparable](name, kind string, extract KeyFunc[T, K], value T) (key K, present bool, err error) {
	defer func() {
		if recover() != nil {
			var zero K
			key, present = zero, false
			err = fmt.Errorf("query: relation %q %s key extractor panicked", name, kind)
		}
	}()
	key, present = extract(value)
	return key, present, nil
}

func cloneRelationValues[T any](values []T) []T {
	if len(values) == 0 {
		return make([]T, 0)
	}
	return append([]T(nil), values...)
}

func relationFailure(name string, chunk int, err error) error {
	return &RelationError{Relation: name, Chunk: chunk, Err: err}
}
