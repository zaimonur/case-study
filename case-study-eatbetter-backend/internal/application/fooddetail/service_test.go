package fooddetail

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/zaimonur/case-study/case-study-eatbetter-backend/internal/domain/food"
)

type stubRepository struct {
	query  Query
	detail Detail
	err    error
}

func (r *stubRepository) Get(_ context.Context, query Query) (Detail, error) {
	r.query = query
	return r.detail, r.err
}

func TestServiceValidatesLocaleAndNormalizesEmptyPortions(t *testing.T) {
	t.Parallel()
	repository := &stubRepository{detail: Detail{Food: food.Food{ID: 7}}}
	got, err := NewService(repository).Get(context.Background(), Request{FoodID: 7, Locale: "TR-tr"})
	if err != nil {
		t.Fatal(err)
	}
	if repository.query != (Query{FoodID: 7, Locale: "tr-TR", BaseLocale: "tr"}) {
		t.Fatalf("query = %+v", repository.query)
	}
	if !reflect.DeepEqual(got.Portions, []food.Portion{}) {
		t.Fatalf("portions = %#v, want empty slice", got.Portions)
	}
}

func TestServiceErrors(t *testing.T) {
	t.Parallel()
	service := NewService(&stubRepository{})
	for _, request := range []Request{{}, {FoodID: -1}, {FoodID: 1, Locale: "tr_TR"}} {
		if _, err := service.Get(context.Background(), request); !IsValidationError(err) {
			t.Errorf("Get(%+v) error = %v, want validation error", request, err)
		}
	}
	if _, err := NewService(&stubRepository{err: ErrNotFound}).Get(context.Background(), Request{FoodID: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("not-found error = %v", err)
	}
}
