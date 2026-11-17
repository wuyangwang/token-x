package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	myui "github.com/wuyangwang/my-ui"
	"github.com/wuyangwang/my-ui/icons"

	"token-x/core"
)

// sessionsPage is the session list: filters on top, the table below, and
// the delete action in a sticky toolbar.
func (a *app) sessionsPage(c *ui.Context, t *ui.Theme) {
	// Filters.
	ui.Row(c).FillWidth().Gap(t.Space(2.5)).AlignItems(ui.Center).Children(func() {
		myui.NewInput(c.Key("query"), &a.fQuery).Outline().Sm().
			Width(t.Space(56)).PrefixIcon(icons.Search()).
			Placeholder("搜索标题…").Label("搜索标题")

		ui.Box(c).Grow(1)

		source := a.fSource
		opts := []myui.SelectOption{{Label: "全部来源", Value: ""}}
		for _, s := range a.reg.Sources {
			opts = append(opts, myui.SelectOption{Label: s.Name(), Value: s.Name()})
		}
		myui.NewSelect(c.Key("source"), &source, opts).Outline().Sm().Width(t.Space(32)).Label("来源")
		if source != a.fSource {
			a.fSource = source
			a.onFilterChange()
		}

		project := a.fProject
		popts := []myui.SelectOption{{Label: "全部项目", Value: ""}}
		for _, p := range core.Distinct(a.session, func(s core.Session) string { return s.Project }) {
			popts = append(popts, myui.SelectOption{Label: shortPath(p), Value: p})
		}
		myui.NewSelect(c.Key("project"), &project, popts).Outline().Sm().Width(t.Space(52)).Label("项目")
		if project != a.fProject {
			a.fProject = project
			a.onFilterChange()
		}

		model := a.fModel
		mopts := []myui.SelectOption{{Label: "全部模型", Value: ""}}
		for _, m := range core.Distinct(a.session, func(s core.Session) string { return s.Model }) {
			mopts = append(mopts, myui.SelectOption{Label: m, Value: m})
		}
		myui.NewSelect(c.Key("model"), &model, mopts).Outline().Sm().Width(t.Space(48)).Label("模型")
		if model != a.fModel {
			a.fModel = model
			a.onFilterChange()
		}
	})

	// Toolbar with the delete action.
	rows := a.filtered()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		summary := fmt.Sprintf("%d 个 session", len(rows))
		if a.chosen.Len() > 0 {
			summary += fmt.Sprintf("，已选 %d 个", a.chosen.Len())
		}
		ui.Text(c, summary).
			FontSize(myui.TextSize(t, myui.TextSm)).TextColor(t.TextMuted)
		ui.Box(c).Grow(1)
		if a.deleting {
			ui.Text(c, "删除中…").TextColor(t.TextMuted)
		}
		// The confirm modal hangs the dialog on the delete button.
		a.confirmModal(c, t)
	})

	// Table.
	sig := a.tableSignature()
	if sig != a.tableSig {
		a.tableSig = sig
		a.chosen.Clear()
	}
	a.rows.Key = func(row int) any { return rows[row].ID }
	cols := []ui.TableColumn{
		{Title: "标题", MinWidth: 220},
		{Title: "来源", Width: 82},
		{Title: "项目", MinWidth: 140},
		{Title: "模型", Width: 150},
		{Title: "更新时间", Width: 110},
		{Title: "Tokens", Width: 100, Align: ui.End},
		{Title: "金额", Width: 100, Align: ui.End},
	}
	a.rows.Selection = &a.chosen
	ui.Table(c, &a.rows, cols, len(rows), func(row, col int) {
		ui.Text(c, sessionCell(rows[row], col)).SingleLine()
	}).Grow(1)
}

func (a *app) onFilterChange() {
	a.chosen.Clear()
	a.tableSig = a.tableSignature()
}

// confirmModal is the delete-confirmation dialog, opened from the toolbar
// delete button.
func (a *app) confirmModal(c *ui.Context, t *ui.Theme) {
	myui.NewModal(c, &a.confirm, func() ui.Element {
		del := myui.NewButton(c.Key("delete"), "永久删除").
			Icon(icons.Trash2()).Destructive().Sm()
		del.Disabled(a.chosen.Len() == 0 || a.deleting)
		return del.Element
	}, func(panel ui.Element) {
		panel.Label("永久删除")
		rows := a.filtered()
		var chosen []core.Session
		for id := range a.chosen.All() {
			for _, s := range rows {
				if s.ID == id {
					chosen = append(chosen, s)
					break
				}
			}
		}
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

func sessionCell(s core.Session, col int) string {
	switch col {
	case 0:
		title := s.Title
		if s.IsSubagent() {
			title = "↳ " + title
		}
		return title
	case 1:
		return s.Source
	case 2:
		return shortPath(s.Project)
	case 3:
		return s.Model
	case 4:
		return s.Updated.Local().Format("01-02 15:04")
	case 5:
		return fmtTokens(s.Tokens.Total())
	case 6:
		c := fmtCost(s.Cost.Amount)
		if s.Cost.Estimated {
			return c + "*"
		}
		return c
	}
	return ""
}
