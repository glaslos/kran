package linkgroup

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/glaslos/kran/internal/config"
)

func TestNormalizeName(t *testing.T) {
	if g, w := NormalizeName("/foo"), "foo"; g != w {
		t.Fatalf("got %q want %q", g, w)
	}
	if g, w := NormalizeName("foo"), "foo"; g != w {
		t.Fatalf("got %q want %q", g, w)
	}
}

func TestClusterByGroup(t *testing.T) {
	members := []Member{
		mkMember("1", "a", "g1"),
		mkMember("2", "b", ""),
		mkMember("3", "c", "g1"),
	}
	cl := ClusterByGroup(members)
	if len(cl["g1"]) != 2 {
		t.Fatalf("g1: got %d want 2", len(cl["g1"]))
	}
	if len(cl) != 1 {
		t.Fatalf("unexpected groups %v", cl)
	}
}

func TestComputeOrders_chain(t *testing.T) {
	// app depends on db: start db then app; stop app then db
	members := []Member{
		mkMemberDep("dbid", "mydb", "g", ""),
		mkMemberDep("appid", "myapp", "g", "mydb"),
	}
	o, err := ComputeOrders(members, nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Ambiguous {
		t.Fatal("unexpected ambiguous")
	}
	if strings.Join(o.Start, ",") != "dbid,appid" {
		t.Fatalf("start order %v", o.Start)
	}
	if strings.Join(o.Stop, ",") != "appid,dbid" {
		t.Fatalf("stop order %v", o.Stop)
	}
}

func TestComputeOrders_diamond(t *testing.T) {
	members := []Member{
		mkMemberDep("db", "db", "g", ""),
		mkMemberDep("cache", "cache", "g", ""),
		mkMemberDep("app", "app", "g", "db,cache"),
	}
	o, err := ComputeOrders(members, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Start) != 3 || o.Start[2] != "app" {
		t.Fatalf("app last: %v", o.Start)
	}
	firstTwo := strings.Join(o.Start[:2], ",")
	if firstTwo != "cache,db" && firstTwo != "db,cache" {
		t.Fatalf("unexpected first two %v", o.Start[:2])
	}
}

func TestComputeOrders_cycle(t *testing.T) {
	members := []Member{
		mkMemberDep("a", "a", "g", "b"),
		mkMemberDep("b", "b", "g", "a"),
	}
	_, err := ComputeOrders(members, nil)
	if err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestComputeOrders_ambiguous(t *testing.T) {
	members := []Member{
		mkMemberDep("x", "x", "g", ""),
		mkMemberDep("y", "y", "g", ""),
	}
	o, err := ComputeOrders(members, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Ambiguous {
		t.Fatal("expected ambiguous")
	}
	if strings.Join(o.Start, ",") != "x,y" {
		t.Fatalf("name order %v", o.Start)
	}
}

func TestComputeOrders_unknownDepCallsCallback(t *testing.T) {
	var seen []string
	members := []Member{
		mkMemberDep("a", "a", "g", "ghost"),
	}
	_, err := ComputeOrders(members, func(dep string) { seen = append(seen, dep) })
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "ghost" {
		t.Fatalf("callback %v", seen)
	}
}

func mkMember(id, name, group string) Member {
	labels := map[string]string{}
	if group != "" {
		labels[config.LabelLinkGroupKey] = group
	}
	return Member{
		ID:   id,
		Name: name,
		Inspect: types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{Name: "/" + name},
			Config:            &container.Config{Labels: labels, Image: "img:latest"},
		},
	}
}

func mkMemberDep(id, name, group, depends string) Member {
	m := mkMember(id, name, group)
	if depends != "" {
		m.Inspect.Config.Labels[config.LabelDependsOnKey] = depends
	}
	return m
}
