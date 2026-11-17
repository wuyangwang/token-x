package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	myui "github.com/wuyangwang/my-ui"
	"github.com/wuyangwang/my-ui/icons"

	"token-x/core"
)

// sessionsSection is the filter row and the table — the working area of
// the window.
func (a *app) sessionsSection(c *ui.Context, t *ui.Theme) {
	// Filters, one compact row.
	ui.Row(c).FillWidth().PaddingX(t.Space(6)).PaddingY(t.Space(3)).
		Gap(t.Space(2.5)).AlignItems(ui.Center).Children(func() {
		myui.NewInput(c.Key("query"), &a.fQuery).Outline().Sm().
			Width(t.Space(44)).PrefixIcon(icons.Search()).
			Placeholder("搜索…").Label("搜索")

		ui.Box(c).Grow(1)

		project := a.fProject
		popts := []myui.SelectOption{{Label: "全部项目", Value: ""}}
		for _, p := range core.Distinct(a.session, func(s core.Session) string { return s.Project }) {
			popts = append(popts, myui.SelectOption{Label: shortPath(p), Value: p})
		}
		myui.NewSelectClear(c.Key("project"), &project, popts, false).Outline().Sm().Width(t.Space(44)).Label("项目")
		if project != a.fProject {
			a.fProject = project
			a.chosen.Clear()
		}

		source := a.fSource
		sopts := []myui.SelectOption{{Label: "全部来源", Value: ""}}
		for _, s := range a.reg.Sources {
			sopts = append(sopts, myui.SelectOption{Label: s.Name(), Value: s.Name()})
		}
		myui.NewSelectClear(c.Key("source"), &source, sopts, false).Outline().Sm().Width(t.Space(36)).Label("来源")
		if source != a.fSource {
			a.fSource = source
			a.chosen.Clear()
		}
	})

	// The table.
	rows := a.filtered()
	a.rowsNow = rows
	a.rows.Key = func(row int) any { return rows[row].ID }
	cols := []ui.TableColumn{
		{Title: "会话", MinWidth: 160},
		{Title: "模型", Width: 110},
		{Title: "Tokens", Width: 75, Align: ui.End},
		{Title: "金额", Width: 72, Align: ui.End},
		{Title: "时间", Width: 88},
	}
	a.rows.Selection = &a.chosen
	ui.Table(c, &a.rows, cols, len(rows), func(row, col int) {
		s := rows[row]
		if col == 1 {
			// The model is clipped to its column; the tooltip keeps the
			// full name within reach.
			ui.Text(c, s.Model).SingleLine().TruncateMiddle().Tooltip(s.Model)
			return
		}
		ui.Text(c, sessionCell(s, col)).SingleLine()
	}).Grow(1)
}

// footer is the bottom bar: the count on the left, the delete action on
// the right when there is something to act on. Tall enough that the
// button never spills over the table above.
func (a *app) footer(c *ui.Context, t *ui.Theme) {
	ui.Column(c).FillWidth().Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(t.Border)
		ui.Row(c).FillWidth().PaddingY(t.Space(3)).PaddingX(t.Space(6)).
			Gap(t.Space(3)).AlignItems(ui.Center).Children(func() {
			count := fmt.Sprintf("%d 个 session", len(a.rowsNow))
			if a.chosen.Len() > 0 {
				count += fmt.Sprintf(" · 已选 %d", a.chosen.Len())
			}
			ui.Text(c, count).FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
			if a.loading {
				ui.Text(c, "读取中…").FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
			}
			if a.deleting {
				ui.Text(c, "删除中…").FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
			}
			ui.Box(c).Grow(1)
			a.confirmModal(c, t)
		})
	})
}

// filtered applies the filters.
func (a *app) filtered() []core.Session {
	out := a.session[:0:0]
	for _, s := range a.session {
		if a.fSource != "" && s.Source != a.fSource {
			continue
		}
		if a.fProject != "" && s.Project != a.fProject {
			continue
		}
		if a.fQuery != "" && !strings.Contains(strings.ToLower(s.Title), strings.ToLower(a.fQuery)) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// confirmModal is the delete-confirmation dialog, opened from the footer
// delete button.
func (a *app) confirmModal(c *ui.Context, t *ui.Theme) {
	myui.NewModal(c, &a.confirm, func() ui.Element {
		del := myui.NewButton(c.Key("delete"), "删除").
			Icon(icons.Trash2()).Destructive().Sm()
		del.Disabled(a.chosen.Len() == 0 || a.deleting)
		return del.Element
	}, func(panel ui.Element) {
		panel.Label("永久删除")
		chosen := a.chosenSessions()
		if len(chosen) == 0 {
			a.confirm = false
			return
		}
		tokens, cost := core.Totals(chosen)

		ui.Column(c).FillWidth().Gap(t.Space(3)).Children(func() {
			ui.Text(c, "永久删除？").FontSize(myui.TextSize(t, myui.TextLg)).Bold()
			ui.Text(c, fmt.Sprintf("将永久删除 %d 个 session（约 %s tokens，%s），此操作不可恢复。",
				len(chosen), fmtTokens(tokens.Total()), fmtCost(cost.Amount)),
			).TextColor(t.TextMuted)
			ui.Column(c).Grow(1).Gap(t.Space(1)).PaddingY(t.Space(1)).Children(func() {
				max := 8
				for i, s := range chosen {
					if i == max {
						ui.Text(c, fmt.Sprintf("…等共 %d 个", len(chosen))).TextColor(t.TextMuted)
						break
					}
					ui.Text(c, "· "+s.Title).SingleLine()
				}
			})

			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Grow(1)
				myui.NewButton(c.Key("cancel"), "取消").Ghost().
					OnClick(func() { a.confirm = false })
				destructive := myui.NewButton(c.Key("confirm delete"), "永久删除").Destructive()
				destructive.Disabled(a.deleting)
				destructive.OnClick(func() { a.deleteChosen(chosen) })
			})
		})
	})
}

// chosenSessions resolves the selection against the current rows.
func (a *app) chosenSessions() []core.Session {
	var out []core.Session
	for id := range a.chosen.All() {
		for _, s := range a.rowsNow {
			if s.ID == id {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// deleteChosen removes the sessions off the UI thread, then reloads.
func (a *app) deleteChosen(chosen []core.Session) {
	a.deleting = true
	a.confirm = false
	win := a.win
	go func() {
		var errs []string
		deleted := 0
		for _, s := range chosen {
			src, err := a.reg.Get(s.Source)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			if err := src.DeleteSession(s.ID); err != nil {
				errs = append(errs, fmt.Sprintf("%s %s: %v", s.Source, s.Title, err))
			} else {
				deleted++
			}
		}
		var toast pendingToast
		if len(errs) > 0 {
			toast = pendingToast{Title: "删除失败", Desc: strings.Join(errs, "\n"), Error: true}
		} else {
			toast = pendingToast{Title: "已永久删除", Desc: fmt.Sprintf("%d 个 session", deleted)}
		}
		win.Update(func() {
			a.deleting = false
			a.chosen.Clear()
			a.toast = toast
			a.reload()
		})
	}()
}

// shortPath trims a project path for display.
func shortPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return p
}

// sourceDot is a tiny square before the title, colored per source — the
// same encoding the legend of a chart would use.
func sourceDot(c *ui.Context, t *ui.Theme, source string) ui.Color {
	switch source {
	case "codex":
		return t.Accent
	case "opencode":
		return t.Success
	}
	return t.TextMuted
}

func sessionCell(s core.Session, col int) string {
	switch col {
	case 0:
		title := s.Title
		if s.IsSubagent() {
			title = "↳ " + title
		}
		return title
	case 1:
		return s.Model
	case 2:
		return fmtTokens(s.Tokens.Total())
	case 3:
		return fmtCost(s.Cost.Amount)
	case 4:
		return s.Updated.Local().Format("01-02 15:04")
	}
	return ""
}
