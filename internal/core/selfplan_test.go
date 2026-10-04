package core

import (
	"reflect"
	"testing"
)

func TestSelfPlanAllowed(t *testing.T) {
	cases := []struct {
		name string
		in   []bool
		want bool
	}{
		{"不在班里", nil, true},
		{"一个班允许", []bool{true}, true},
		{"一个班不允许", []bool{false}, false},
		{"多班都允许", []bool{true, true}, true},
		{"多班取最严", []bool{true, false, true}, false},
	}
	for _, c := range cases {
		if got := SelfPlanAllowed(c.in); got != c.want {
			t.Errorf("%s: SelfPlanAllowed(%v) = %v", c.name, c.in, got)
		}
	}
}

func TestPlanBooksAllowed(t *testing.T) {
	cases := []struct {
		name    string
		books   []string
		targets [][]string
		want    []string
	}{
		{"没有对象", []string{"a"}, nil, []string{}},
		{"目标为空不约束", []string{"a", "b"}, [][]string{{}}, []string{}},
		{"在目标里", []string{"a"}, [][]string{{"a", "b"}}, []string{}},
		{"不在目标里", []string{"a", "c", "c"}, [][]string{{"a", "b"}}, []string{"c"}},
		{"多对象取交集", []string{"a", "b"}, [][]string{{"a", "b"}, {"a"}}, []string{"b"}},
		{"多对象交集都满足", []string{"a"}, [][]string{{"a", "b"}, {"c", "a"}}, []string{}},
		{"空目标的对象不参与交集", []string{"b"}, [][]string{{"a", "b"}, {}}, []string{}},
		{"交集为空时全部不满足", []string{"a", "b"}, [][]string{{"a"}, {"b"}}, []string{"a", "b"}},
	}
	for _, c := range cases {
		if got := PlanBooksAllowed(c.books, c.targets); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: PlanBooksAllowed = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAllowedPlanBooks(t *testing.T) {
	cases := []struct {
		name        string
		targets     [][]string
		want        []string
		constrained bool
	}{
		{"没有对象", nil, []string{}, false},
		{"都为空", [][]string{{}, {}}, []string{}, false},
		{"一个对象", [][]string{{"b", "a"}}, []string{"b", "a"}, true},
		{"交集按第一个非空集合的顺序", [][]string{{}, {"c", "b", "a"}, {"a", "c"}}, []string{"c", "a"}, true},
		{"交集为空", [][]string{{"a"}, {"b"}}, []string{}, true},
	}
	for _, c := range cases {
		got, constrained := AllowedPlanBooks(c.targets)
		if !reflect.DeepEqual(got, c.want) || constrained != c.constrained {
			t.Errorf("%s: AllowedPlanBooks = %v %v, want %v %v", c.name, got, constrained, c.want, c.constrained)
		}
	}
}
