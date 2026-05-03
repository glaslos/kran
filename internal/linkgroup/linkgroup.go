package linkgroup

import (
	"container/heap"
	"fmt"
	"sort"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/glaslos/kran/internal/config"
)

// Member is one managed running container in a link group.
type Member struct {
	ID      string
	Name    string
	Inspect types.ContainerJSON
}

// NormalizeName matches Docker list/inspect names (optional leading slash).
func NormalizeName(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "/")
}

// GroupFromLabels returns the link group id if the container opts into grouping.
func GroupFromLabels(labels map[string]string) (group string, ok bool) {
	if labels == nil {
		return "", false
	}
	g := strings.TrimSpace(labels[config.LabelLinkGroupKey])
	if g == "" {
		return "", false
	}
	return g, true
}

// NewMember builds a Member from inspect data.
func NewMember(id string, in types.ContainerJSON) Member {
	return Member{
		ID:      id,
		Name:    NormalizeName(in.Name),
		Inspect: in,
	}
}

// ClusterByGroup partitions members (same daemon, typically all managed) by link_group label.
func ClusterByGroup(members []Member) map[string][]Member {
	out := make(map[string][]Member)
	for _, m := range members {
		if m.Inspect.Config == nil {
			continue
		}
		g, ok := GroupFromLabels(m.Inspect.Config.Labels)
		if !ok {
			continue
		}
		out[g] = append(out[g], m)
	}
	return out
}

// ParseDependsOn splits the kran.depends_on label.
func ParseDependsOn(labels map[string]string) []string {
	if labels == nil {
		return nil
	}
	raw := strings.TrimSpace(labels[config.LabelDependsOnKey])
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var deps []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			deps = append(deps, p)
		}
	}
	return deps
}

// Orders holds container IDs in stop and start sequence.
type Orders struct {
	// Start is dependency-first (dependencies before dependents).
	Start []string
	// Stop is dependents-first (reverse of dependency order).
	Stop []string
	// Ambiguous is true when the group has more than one member but no dependency edges among members.
	Ambiguous bool
}

// ComputeOrders builds stop/start orders from dependency edges among members only.
// unknownDep is called for each depends_on name that does not resolve to another member.
func ComputeOrders(members []Member, unknownDep func(dep string)) (Orders, error) {
	var o Orders
	if len(members) == 0 {
		return o, nil
	}

	byID := make(map[string]Member, len(members))
	nameToID := make(map[string]string, len(members))
	for _, m := range members {
		byID[m.ID] = m
		nameToID[m.Name] = m.ID
	}

	// directed edge: prerequisite -> dependent (prereq must start before dependent)
	type edge struct{ from, to string }
	var edges []edge
	edgeSet := make(map[string]struct{})
	addEdge := func(from, to string) {
		if from == to {
			return
		}
		key := from + "\x00" + to
		if _, ok := edgeSet[key]; ok {
			return
		}
		edgeSet[key] = struct{}{}
		edges = append(edges, edge{from, to})
	}

	for _, m := range members {
		for _, dep := range ParseDependsOn(m.Inspect.Config.Labels) {
			depName := NormalizeName(dep)
			prereqID, ok := nameToID[depName]
			if !ok {
				if unknownDep != nil {
					unknownDep(dep)
				}
				continue
			}
			addEdge(prereqID, m.ID)
		}
	}

	o.Ambiguous = len(members) > 1 && len(edges) == 0

	// Kahn topological sort; stable tie-break by container name.
	indeg := make(map[string]int, len(members))
	adj := make(map[string][]string)
	for id := range byID {
		indeg[id] = 0
	}
	for _, e := range edges {
		indeg[e.to]++
		adj[e.from] = append(adj[e.from], e.to)
	}
	for from := range adj {
		sort.Strings(adj[from])
	}

	pq := make(idHeap, 0)
	for id := range byID {
		if indeg[id] == 0 {
			heap.Push(&pq, heapItem{id: id, name: byID[id].Name})
		}
	}

	var start []string
	for pq.Len() > 0 {
		id := heap.Pop(&pq).(heapItem).id
		start = append(start, id)
		for _, to := range adj[id] {
			indeg[to]--
			if indeg[to] == 0 {
				heap.Push(&pq, heapItem{id: to, name: byID[to].Name})
			}
		}
	}

	if len(start) != len(members) {
		return Orders{}, fmt.Errorf("link group has a cyclic kran.depends_on")
	}

	o.Start = start
	o.Stop = reverseCopy(start)
	return o, nil
}

func reverseCopy(ids []string) []string {
	out := make([]string, len(ids))
	for i := range ids {
		out[i] = ids[len(ids)-1-i]
	}
	return out
}

type heapItem struct {
	id, name string
}

// idHeap is a min-heap of items ordered by name then id.
type idHeap []heapItem

func (h idHeap) Len() int { return len(h) }

func (h idHeap) Less(i, j int) bool {
	if h[i].name != h[j].name {
		return h[i].name < h[j].name
	}
	return h[i].id < h[j].id
}

func (h idHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *idHeap) Push(x any) {
	*h = append(*h, x.(heapItem))
}

func (h *idHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}
