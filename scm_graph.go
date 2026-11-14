package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

const (
	scmGraphHeaderHeight         = 30
	scmGraphRowHeight            = 28
	scmGraphSashHeight           = 5
	defaultSCMGraphRatio float32 = 0.42
)

func scmGraphHeight(ratio, available float32) float32 {
	if ratio <= 0 {
		ratio = defaultSCMGraphRatio
	}
	space := max(0, available-scmGraphSashHeight)
	minimum := min(float32(90), space/2)
	return min(max(space*ratio, minimum), space-min(float32(140), space/2))
}

func (a *App) scmGraphDivider(c *ui.Context, k *colors, height, available float32) {
	div := ui.Box(c.Key("scm-graph-sash")).Height(scmGraphSashHeight).Shrink(0).
		Role(ui.RoleSplitter).Label("Resize Source Control Graph").Cursor(ui.CursorResizeRow).
		Tooltip("Drag to resize graph; double-click to reset")
	if _, dy, ok := div.Dragged(); ok && dy != 0 && available > scmGraphSashHeight {
		space := available - scmGraphSashHeight
		prefs.SCMGraphRatio = scmGraphHeight(max(0.001, (height-dy)/space), available) / space
		saveSettings()
		c.Invalidate()
	}
	if div.DoubleClicked() {
		prefs.SCMGraphRatio = defaultSCMGraphRatio
		saveSettings()
		c.Invalidate()
	}
	div.Draw(func(p *ui.Painter, r ui.Rect) {
		color := k.panelBorder
		h := float32(1)
		if div.Hovered() || div.Dragging() || div.Pressed() {
			color, h = ui.Hex("#3b8eea"), 3
		}
		p.Fill(ui.Rect{X: r.X, Y: r.Y + (r.H-h)/2, W: r.W, H: h}, color, 0)
	})
}

type scmGraphLane struct {
	id    string
	color int
}
type scmGraphEdge struct {
	from, to    int
	top, bottom float32
	color       int
}
type scmGraphRow struct {
	commit             gitCommit
	lane, color, lanes int
	edges              []scmGraphEdge
}

// Track parent identities through the topological history. Existing lanes
// retain their colors as branches split, merge, and shift left.
func scmGraphLayout(history *gitHistory) []scmGraphRow {
	var rows []scmGraphRow
	var lanes []scmGraphLane
	nextColor := 0
	index := func(id string) int {
		return slices.IndexFunc(lanes, func(lane scmGraphLane) bool { return lane.id == id })
	}
	for _, commit := range history.Commits {
		if commit.Continuation {
			continue
		}
		id := commit.ID
		if id == "" {
			id = commit.Hash
		}
		lane := index(id)
		incoming := lane >= 0
		if !incoming {
			lane = len(lanes)
			lanes = append(lanes, scmGraphLane{id, nextColor})
			nextColor++
		}
		before := slices.Clone(lanes)
		row := scmGraphRow{commit: commit, lane: lane, color: lanes[lane].color, lanes: len(lanes)}
		lanes = slices.Delete(lanes, lane, lane+1)
		for i, parent := range commit.Parents {
			if index(parent) >= 0 {
				continue
			}
			color := row.color
			if i > 0 {
				color = nextColor
				nextColor++
			}
			at := min(lane+i, len(lanes))
			lanes = slices.Insert(lanes, at, scmGraphLane{parent, color})
		}
		row.lanes = max(row.lanes, len(lanes))
		for from, old := range before {
			if from == lane {
				continue
			}
			if to := index(old.id); to >= 0 {
				row.edges = append(row.edges, scmGraphEdge{from, to, 0, scmGraphRowHeight, old.color})
			}
		}
		if incoming {
			row.edges = append(row.edges, scmGraphEdge{lane, lane, 0, scmGraphRowHeight / 2, row.color})
		}
		for _, parent := range commit.Parents {
			if to := index(parent); to >= 0 {
				row.edges = append(row.edges, scmGraphEdge{lane, to, scmGraphRowHeight / 2, scmGraphRowHeight, lanes[to].color})
			}
		}
		rows = append(rows, row)
	}
	return rows
}

func scmLaneColor(index int) ui.Color {
	palette := [...]string{"#3b8eea", "#e5b567", "#b294e2", "#73c991", "#e06c75", "#56b6c2"}
	return ui.Hex(palette[index%len(palette)])
}

func paintSCMGraph(p *ui.Painter, r ui.Rect, row scmGraphRow, background ui.Color) {
	x := func(lane int) float32 { return r.X + 10 + float32(lane)*14 }
	for _, edge := range row.edges {
		var path ui.Path
		from, to := x(edge.from), x(edge.to)
		top, bottom := r.Y+edge.top, r.Y+edge.bottom
		path.MoveTo(from, top)
		if from == to {
			path.LineTo(to, bottom)
		} else {
			// Lane changes curve through the lower half so neighboring rows join.
			bend := max(top, r.Y+scmGraphRowHeight/2)
			path.LineTo(from, bend).CubeTo(from, bend+7, to, bottom-7, to, bottom)
		}
		p.StrokePath(&path, 1.5, scmLaneColor(edge.color))
	}
	centerX, centerY := x(row.lane), r.Y+scmGraphRowHeight/2
	color := scmLaneColor(row.color)
	if strings.Contains(row.commit.Decorations, "HEAD") {
		p.Fill(ui.Rect{X: centerX - 5, Y: centerY - 5, W: 10, H: 10}, background, 5)
		p.Stroke(ui.Rect{X: centerX - 5, Y: centerY - 5, W: 10, H: 10}, color, 5, 1.5)
		p.Fill(ui.Rect{X: centerX - 2, Y: centerY - 2, W: 4, H: 4}, color, 2)
	} else {
		p.Fill(ui.Rect{X: centerX - 3.5, Y: centerY - 3.5, W: 7, H: 7}, color, 3.5)
	}
}

func (a *App) scmGraph(c *ui.Context, k *colors, p *Pane, repo *gitRepo) {
	history := a.gitHistoryFor(c, p, repo)
	expanded := a.scmGroupExpanded("graph", true)
	header := ui.Row(c).Height(scmGraphHeaderHeight).Shrink(0).Padding(0, 8).Gap(5).AlignItems(ui.Center).
		Role(ui.RoleButton).Label("Toggle Source Control Graph").Cursor(ui.CursorPointer)
	acted := false
	header.Children(func() {
		ui.Icon(c, icon(scmChevron(expanded))).Size(12, 12).TextColor(k.textMuted)
		ui.Text(c, "Graph").FontSize(11).FontWeight(600).TextColor(k.textMuted).Grow(1)
		if history != nil {
			ui.Text(c, fmt.Sprint(scmCommitCount(history))).FontSize(10).TextColor(k.textFaint)
		}
		if iconButton(c, k, "rotate-ccw", "Refresh Graph", 22, 12).Tooltip("Refresh graph").Clicked() {
			acted = true
			a.invalidateGitHistory(repo.Dir)
			c.Invalidate()
		}
	})
	if header.Clicked() && !acted {
		a.scmExpanded["graph"] = !expanded
		c.Invalidate()
	}
	if !expanded {
		return
	}
	if history == nil {
		ui.Text(c, "Loading history…").FontSize(12).TextColor(k.textFaint).Padding(8, 12)
		return
	}
	if len(history.Commits) == 0 {
		ui.Text(c, "No commits yet").FontSize(12).TextColor(k.textFaint).Padding(8, 12)
		return
	}
	if a.scmGraphHistory != history {
		a.scmGraphHistory = history
		a.scmGraphRows = scmGraphLayout(history)
	}
	ui.Scroll(c.Key(repo.Dir)).Label("Source Control Graph History").Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Gap(0).Children(func() {
			for _, row := range a.scmGraphRows {
				a.scmGraphCommitRow(c, k, row)
			}
		})
	})
}

func scmCommitCount(history *gitHistory) int {
	count := 0
	for _, commit := range history.Commits {
		if !commit.Continuation {
			count++
		}
	}
	return count
}

func (a *App) scmGraphCommitRow(c *ui.Context, k *colors, graph scmGraphRow) {
	commit := graph.commit
	row := ui.Row(c.Key(commit.Hash)).Height(scmGraphRowHeight).Shrink(0).Padding(0, 8, 0, 4).Gap(5).AlignItems(ui.Center).
		Label("Commit " + commit.Hash).Role(ui.RoleButton).Cursor(ui.CursorPointer).
		Tooltip(commit.Subject + "\n" + commit.Author + " · " + commit.Hash)
	background := k.panel
	if a.scmGraphSelected == commit.Hash {
		background = k.panelSel
	} else if row.Hovered() {
		background = k.hover
	}
	row.Background(background)
	if row.Clicked() {
		a.scmGraphSelected = commit.Hash
		c.Invalidate()
	}
	row.Children(func() {
		ui.Box(c).Width(float32(max(2, graph.lanes))*14 + 6).Height(scmGraphRowHeight).Shrink(0).
			Draw(func(p *ui.Painter, r ui.Rect) { paintSCMGraph(p, r, graph, background) })
		ui.Text(c, commit.Subject).FontSize(12).TextColor(k.text).SingleLine().Ellipsis("…").MinWidth(0).Grow(1)
		for _, decoration := range scmDecorations(commit.Decorations) {
			glyph := "git-branch"
			if strings.Contains(commit.Decorations, "tag: "+decoration.label) {
				glyph = "tag"
			}
			ui.Row(c).Gap(3).Padding(1, 4).Radius(3).Background(decoration.color.Alpha(0.12)).MaxWidth(105).Shrink(1).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon(glyph)).Size(10, 10).TextColor(decoration.color).Shrink(0)
				ui.Text(c, decoration.label).FontSize(10).TextColor(decoration.color).SingleLine().Ellipsis("…").MinWidth(0)
			})
		}
	})
}
