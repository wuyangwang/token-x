package main

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/chart"
	myui "github.com/wuyangwang/my-ui"

	"token-x/core"
)

// dashboardPage is the overview: today's cards, the cost trend, and the
// per-model breakdown.
func (a *app) dashboardPage(c *ui.Context, t *ui.Theme) {
	today := time.Now().Local().Truncate(24 * time.Hour)

	var todaySessions []core.Session
	for _, s := range a.session {
		if s.Updated.Local().Truncate(24*time.Hour).Equal(today) {
			todaySessions = append(todaySessions, s)
		}
	}
	todayTokens, todayCost := core.Totals(todaySessions)
	allTokens, allCost := core.Totals(a.session)

	ci := ui.Column(c).FillWidth().Gap(t.Space(4))
	ci.Children(func() {
		// Cards.
		ui.Row(c).FillWidth().Gap(t.Space(4)).Children(func() {
			a.statCard(c, t, "今日 Tokens", fmtTokens(todayTokens.Total()), fmt.Sprintf("in %s · out %s", fmtTokens(todayTokens.Input), fmtTokens(todayTokens.Output)))
			costLabel := fmtCost(todayCost.Amount)
			if todayCost.Estimated {
				costLabel += " 估算"
			}
			a.statCard(c, t, "今日金额", costLabel, fmt.Sprintf("%d 个 session", len(todaySessions)))
			a.statCard(c, t, "累计 Tokens", fmtTokens(allTokens.Total()), fmt.Sprintf("in %s · out %s", fmtTokens(allTokens.Input), fmtTokens(allTokens.Output)))
			totalLabel := fmtCost(allCost.Amount)
			if allCost.Estimated {
				totalLabel += " 估算"
			}
			a.statCard(c, t, "累计金额", totalLabel, fmt.Sprintf("%d 个 session", len(a.session)))
		})

		// Last 7 days.
		weekStart := today.AddDate(0, 0, -6)
		var weekSessions []core.Session
		for _, s := range a.session {
			if !s.Updated.Local().Truncate(24*time.Hour).Before(weekStart) {
				weekSessions = append(weekSessions, s)
			}
		}
		weekTokens, weekCost := core.Totals(weekSessions)
		ui.Row(c).FillWidth().Gap(t.Space(4)).Children(func() {
			weekLabel := fmtCost(weekCost.Amount)
			if weekCost.Estimated {
				weekLabel += " 估算"
			}
			a.statCard(c, t, "近 7 天 Tokens", fmtTokens(weekTokens.Total()), fmt.Sprintf("in %s · out %s", fmtTokens(weekTokens.Input), fmtTokens(weekTokens.Output)))
			a.statCard(c, t, "近 7 天金额", weekLabel, fmt.Sprintf("%d 个 session", len(weekSessions)))
			a.statCard(c, t, "近 7 天日均金额", fmtCost(weekCost.Amount/7), "按 7 天平摊")
			a.statCard(c, t, "近 7 天日均 Tokens", fmtTokens(weekTokens.Total()/7), "按 7 天平摊")
		})

		// Trend.
		days := core.DailyTotals(a.session, a.trendDays, time.Now())
		a.trendCard(c, t, days)

		// Model breakdown.
		a.breakdownCard(c, t, todaySessions)
	})
}

// statCard is one number with a caption, in a card.
func (a *app) statCard(c *ui.Context, t *ui.Theme, caption, value, sub string) {
	ui.Column(c).Grow(1).Background(t.Background).Border(1, t.Border).
		Radius(t.Space(2.5)).Padding(t.Space(4), t.Space(4)).Gap(t.Space(1)).Children(func() {
		ui.Text(c, caption).FontSize(myui.TextSize(t, myui.TextSm)).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(myui.TextSize(t, myui.TextXl)).Bold()
		ui.Text(c, sub).FontSize(myui.TextSize(t, myui.TextXs)).TextColor(t.TextMuted)
	})
}

// trendCard draws the daily cost bars for the last 7 or 30 days.
func (a *app) trendCard(c *ui.Context, t *ui.Theme, days []core.DailyUsage) {
	myui.NewCardPanel(c).Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, "每日金额趋势").FontSize(myui.TextSize(t, myui.TextLg)).Bold()
			ui.Box(c).Grow(1)
			for _, n := range []int{7, 30} {
				label := fmt.Sprintf("%d 天", n)
				if a.trendDays == n {
					myui.NewBadge(c, label).Primary()
				} else if btn := myui.NewButton(c.Key(fmt.Sprintf("trend-%d", n)), label).Ghost().Sm(); btn.Clicked() {
					a.trendDays = n
				}
			}
		})

		labels := make([]string, len(days))
		values := make([]float64, len(days))
		for i, d := range days {
			labels[i] = d.Day.Format("01-02")
			values[i] = d.Cost.Amount
		}
		_, hi := chart.Extent(values...)
		y := chart.NewLinear(0, hi).Nice(3)
		x := chart.NewBand(labels...).Padding(0.25, 0.05)
		ticks := y.Ticks(3)

		chart.Plot(c, chart.Axes{Left: ticks, Bottom: x.Ticks()}, func(area ui.Element) {
			area.Draw(func(p *ui.Painter, r ui.Rect) {
				for _, tick := range ticks {
					p.Line(r.X, chart.Y(r, tick.At), r.X+r.W, chart.Y(r, tick.At), 1, t.Border)
				}
			})
			for i, d := range days {
				if d.Cost.Amount <= 0 {
					continue
				}
				tip := fmt.Sprintf("%s  %s\n%s", d.Day.Format("01-02"), fmtCost(d.Cost.Amount), fmtTokens(d.Tokens.Total())+" tokens")
				chart.Rect(c, x.Band(i), y.Span(0, d.Cost.Amount)).
					Radius(2).Background(t.Accent).Tooltip(tip)
			}
		}).Height(180).FillWidth()
	})
}

// breakdownCard is the per-model usage table, for today or in total.
func (a *app) breakdownCard(c *ui.Context, t *ui.Theme, todaySessions []core.Session) {
	myui.NewCardPanel(c).Children(func() {
		ui.Text(c, "按模型细分").FontSize(myui.TextSize(t, myui.TextLg)).Bold()
		selected := myui.NewTabs(c, &a.dayTab, myui.Tab{Label: "今日"}, myui.Tab{Label: "累计"})
		_ = selected
		sessions := a.session
		if a.dayTab == 0 {
			sessions = todaySessions
		}
		rows := core.ModelTotals(sessions)
		if len(rows) == 0 {
			ui.Text(c, "没有用量记录").TextColor(t.TextMuted)
			return
		}
		cols := []ui.TableColumn{
			{Title: "模型", Width: 180},
			{Title: "Session 数", Width: 90, Align: ui.End},
			{Title: "Input", Width: 100, Align: ui.End},
			{Title: "Cached", Width: 100, Align: ui.End},
			{Title: "Output", Width: 100, Align: ui.End},
			{Title: "金额", Width: 110, Align: ui.End},
		}
		ui.Table(c, nil, cols, len(rows), func(row, col int) {
			r := rows[row]
			ui.Text(c, modelCell(r, col)).SingleLine()
		}).Height(t.Space(40))
	})
}

func modelCell(r core.ModelTotal, col int) string {
	switch col {
	case 0:
		return r.Model
	case 1:
		return fmt.Sprintf("%d", r.Count)
	case 2:
		return fmtTokens(r.Tokens.Input)
	case 3:
		return fmtTokens(r.Tokens.CachedIn)
	case 4:
		return fmtTokens(r.Tokens.Output)
	case 5:
		s := fmtCost(r.Cost.Amount)
		if r.Cost.Estimated {
			return s + "*"
		}
		return s
	}
	return ""
}
