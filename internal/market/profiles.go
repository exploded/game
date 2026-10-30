package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/exploded/game/internal/db"
)

// profileMaxAge is how old a stock's profile can get before RefreshProfiles
// fetches it again. Six and a half days, so a run once a day refreshes the
// whole list in one go each week rather than a few stocks every day.
const profileMaxAge = 6*24*time.Hour + 12*time.Hour

// ProfileCutoff is the updated_at before which a profile is due for a
// refresh, in stock_profiles' format.
func ProfileCutoff(now time.Time) string {
	return now.UTC().Add(-profileMaxAge).Format(time.DateTime)
}

// RefreshProfiles fetches company details (/profile), metrics (/statistics)
// and the logo (/logo) for S&P 500 stocks whose profile is missing or older
// than profileMaxAge. None of these endpoints accept batches, and together
// they cost 61 credits per stock, so a full refresh of ~520 stocks takes about
// 53 minutes of the plan's quota.
//
// A failed endpoint leaves that part of the stored profile as it was.
func (f *Fetcher) RefreshProfiles(ctx context.Context, q *db.Queries) error {
	stocks, err := q.ListStocksNeedingProfile(ctx, db.ListStocksNeedingProfileParams{
		Market: "sp500", UpdatedAt: ProfileCutoff(time.Now()),
	})
	if err != nil {
		return err
	}
	stocks = LimitStocks(stocks)
	if len(stocks) == 0 {
		return nil
	}

	slog.Info("profile refresh starting", "stocks", len(stocks))
	start := time.Now()
	ok, failed := 0, 0
	for i, s := range stocks {
		if err := f.refreshProfile(ctx, q, s); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			failed++
			slog.Warn("profile refresh", "symbol", s.Symbol, "error", err)
		} else {
			ok++
		}
		if (i+1)%100 == 0 {
			slog.Info("profile refresh progress", "done", i+1, "total", len(stocks))
		}
	}
	slog.Info("profile refresh complete",
		"ok", ok, "failed", failed,
		"duration_ms", time.Since(start).Milliseconds())
	return nil
}

// refreshProfile updates one stock. It returns an error only if both
// /profile and /statistics failed.
func (f *Fetcher) refreshProfile(ctx context.Context, q *db.Queries, s db.Stock) error {
	var p tdProfile
	profileErr := f.getJSON(ctx, "profile", 10, s.Symbol, &p)
	if profileErr == nil {
		profileErr = q.UpsertStockProfileInfo(ctx, db.UpsertStockProfileInfoParams{
			StockID:     s.ID,
			Exchange:    p.Exchange,
			Industry:    p.Industry,
			Description: p.Description,
			Type:        p.Type,
			Ceo:         p.CEO,
			Employees:   p.Employees,
			Website:     p.Website,
			Address:     p.Address,
			City:        p.City,
			State:       p.State,
			Country:     p.Country,
			Phone:       p.Phone,
		})
	}

	var st tdStatistics
	statsErr := f.getJSON(ctx, "statistics", 50, s.Symbol, &st)
	if statsErr == nil {
		statsErr = q.UpsertStockStatistics(ctx, st.params(s.ID))
	}

	if profileErr != nil && statsErr != nil {
		return fmt.Errorf("profile: %v; statistics: %v", profileErr, statsErr)
	}

	var logo struct {
		URL string `json:"url"`
	}
	if err := f.getJSON(ctx, "logo", 1, s.Symbol, &logo); err == nil && logo.URL != "" {
		q.UpdateStockLogo(ctx, db.UpdateStockLogoParams{Logo: logo.URL, StockID: s.ID})
	}
	return nil
}

// getJSON calls a Twelve Data endpoint that costs the given credits for one
// symbol, and decodes the response into out.
func (f *Fetcher) getJSON(ctx context.Context, endpoint string, cost int, symbol string, out any) error {
	if err := takeCredits(ctx, cost); err != nil {
		return err
	}
	u := fmt.Sprintf("https://api.twelvedata.com/%s?symbol=%s&apikey=%s",
		endpoint, url.QueryEscape(symbol), f.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return err
	}
	var e struct {
		Status  string `json:"status"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	json.Unmarshal(raw, &e)
	if e.Status == "error" {
		return fmt.Errorf("%s %d: %s", endpoint, e.Code, e.Message)
	}
	return json.Unmarshal(raw, out)
}

type tdProfile struct {
	Exchange    string `json:"exchange"`
	Industry    string `json:"industry"`
	Description string `json:"description"`
	Type        string `json:"type"`
	CEO         string `json:"CEO"`
	Employees   int64  `json:"employees"`
	Website     string `json:"website"`
	Address     string `json:"address"`
	City        string `json:"city"`
	State       string `json:"state"`
	Country     string `json:"country"`
	Phone       string `json:"phone"`
}

// tdStatistics mirrors Twelve Data's /statistics response. Numbers are
// float64 because the API sends raw dollars, ratios and fractions alike;
// params scales them to the integer units in sql/schema.sql.
type tdStatistics struct {
	Statistics struct {
		Valuations struct {
			MarketCap       float64 `json:"market_capitalization"`
			EnterpriseValue float64 `json:"enterprise_value"`
			TrailingPE      float64 `json:"trailing_pe"`
			ForwardPE       float64 `json:"forward_pe"`
			PEG             float64 `json:"peg_ratio"`
			PriceToSales    float64 `json:"price_to_sales_ttm"`
			PriceToBook     float64 `json:"price_to_book_mrq"`
			EVToRevenue     float64 `json:"enterprise_to_revenue"`
			EVToEBITDA      float64 `json:"enterprise_to_ebitda"`
		} `json:"valuations_metrics"`
		Financials struct {
			FiscalYearEnds    string  `json:"fiscal_year_ends"`
			MostRecentQuarter string  `json:"most_recent_quarter"`
			GrossMargin       float64 `json:"gross_margin"`
			ProfitMargin      float64 `json:"profit_margin"`
			OperatingMargin   float64 `json:"operating_margin"`
			ReturnOnAssets    float64 `json:"return_on_assets_ttm"`
			ReturnOnEquity    float64 `json:"return_on_equity_ttm"`
			Income            struct {
				Revenue             float64 `json:"revenue_ttm"`
				RevenuePerShare     float64 `json:"revenue_per_share_ttm"`
				QuarterlyRevGrowth  float64 `json:"quarterly_revenue_growth"`
				GrossProfit         float64 `json:"gross_profit_ttm"`
				EBITDA              float64 `json:"ebitda"`
				NetIncome           float64 `json:"net_income_to_common_ttm"`
				DilutedEPS          float64 `json:"diluted_eps_ttm"`
				QuarterlyEarnGrowth float64 `json:"quarterly_earnings_growth_yoy"`
			} `json:"income_statement"`
			Balance struct {
				TotalCash         float64 `json:"total_cash_mrq"`
				CashPerShare      float64 `json:"total_cash_per_share_mrq"`
				TotalDebt         float64 `json:"total_debt_mrq"`
				DebtToEquity      float64 `json:"total_debt_to_equity_mrq"` // a percentage
				CurrentRatio      float64 `json:"current_ratio_mrq"`
				BookValuePerShare float64 `json:"book_value_per_share_mrq"`
			} `json:"balance_sheet"`
			CashFlow struct {
				Operating float64 `json:"operating_cash_flow_ttm"`
				Free      float64 `json:"levered_free_cash_flow_ttm"`
			} `json:"cash_flow"`
		} `json:"financials"`
		Stock struct {
			SharesOutstanding   float64 `json:"shares_outstanding"`
			FloatShares         float64 `json:"float_shares"`
			Avg10Volume         float64 `json:"avg_10_volume"`
			Avg90Volume         float64 `json:"avg_90_volume"`
			SharesShort         float64 `json:"shares_short"`
			ShortRatio          float64 `json:"short_ratio"`
			ShortPctOutstanding float64 `json:"short_percent_of_shares_outstanding"`
			PctInsiders         float64 `json:"percent_held_by_insiders"`
			PctInstitutions     float64 `json:"percent_held_by_institutions"`
		} `json:"stock_statistics"`
		Price struct {
			Week52Low    float64 `json:"fifty_two_week_low"`
			Week52High   float64 `json:"fifty_two_week_high"`
			Week52Change float64 `json:"fifty_two_week_change"` // a percentage
			Beta         float64 `json:"beta"`
			Day50MA      float64 `json:"day_50_ma"`
			Day200MA     float64 `json:"day_200_ma"`
		} `json:"stock_price_summary"`
		Dividends struct {
			ForwardRate     float64 `json:"forward_annual_dividend_rate"`
			ForwardYield    float64 `json:"forward_annual_dividend_yield"`
			TrailingRate    float64 `json:"trailing_annual_dividend_rate"`
			TrailingYield   float64 `json:"trailing_annual_dividend_yield"`
			AvgYield5y      float64 `json:"5_year_average_dividend_yield"` // a percentage
			PayoutRatio     float64 `json:"payout_ratio"`
			Frequency       string  `json:"dividend_frequency"`
			DividendDate    string  `json:"dividend_date"`
			ExDividendDate  string  `json:"ex_dividend_date"`
			LastSplitFactor string  `json:"last_split_factor"`
			LastSplitDate   string  `json:"last_split_date"`
		} `json:"dividends_and_splits"`
	} `json:"statistics"`
}

// Scalers for the integer units in the stock_profiles schema.
func whole(v float64) int64 { return int64(math.Round(v)) }         // raw dollars, counts
func x100(v float64) int64  { return int64(math.Round(v * 100)) }   // ratios, cents
func bps(v float64) int64   { return int64(math.Round(v * 10000)) } // fractions as basis points

func (t tdStatistics) params(stockID int64) db.UpsertStockStatisticsParams {
	s := t.Statistics
	v, fin, inc, bal, cf := s.Valuations, s.Financials, s.Financials.Income, s.Financials.Balance, s.Financials.CashFlow
	st, pr, dv := s.Stock, s.Price, s.Dividends
	return db.UpsertStockStatisticsParams{
		StockID:             stockID,
		MarketCap:           whole(v.MarketCap),
		EnterpriseValue:     whole(v.EnterpriseValue),
		TrailingPe:          x100(v.TrailingPE),
		ForwardPe:           x100(v.ForwardPE),
		PegRatio:            x100(v.PEG),
		PriceToBook:         x100(v.PriceToBook),
		PriceToSales:        x100(v.PriceToSales),
		EvToRevenue:         x100(v.EVToRevenue),
		EvToEbitda:          x100(v.EVToEBITDA),
		Revenue:             whole(inc.Revenue),
		RevenuePerShare:     x100(inc.RevenuePerShare),
		QuarterlyRevGrowth:  bps(inc.QuarterlyRevGrowth),
		GrossProfit:         whole(inc.GrossProfit),
		Ebitda:              whole(inc.EBITDA),
		NetIncome:           whole(inc.NetIncome),
		DilutedEps:          x100(inc.DilutedEPS),
		QuarterlyEarnGrowth: bps(inc.QuarterlyEarnGrowth),
		TotalCash:           whole(bal.TotalCash),
		CashPerShare:        x100(bal.CashPerShare),
		TotalDebt:           whole(bal.TotalDebt),
		DebtToEquity:        whole(bal.DebtToEquity), // percentage -> ratio * 100
		CurrentRatio:        x100(bal.CurrentRatio),
		BookValuePerShare:   x100(bal.BookValuePerShare),
		OperatingCashFlow:   whole(cf.Operating),
		FreeCashFlow:        whole(cf.Free),
		GrossMargin:         bps(fin.GrossMargin),
		OperatingMargin:     bps(fin.OperatingMargin),
		ProfitMargin:        bps(fin.ProfitMargin),
		ReturnOnEquity:      bps(fin.ReturnOnEquity),
		ReturnOnAssets:      bps(fin.ReturnOnAssets),
		SharesOutstanding:   whole(st.SharesOutstanding),
		FloatShares:         whole(st.FloatShares),
		Avg10Volume:         whole(st.Avg10Volume),
		Avg90Volume:         whole(st.Avg90Volume),
		SharesShort:         whole(st.SharesShort),
		ShortRatio:          x100(st.ShortRatio),
		ShortPctOutstanding: bps(st.ShortPctOutstanding),
		PctHeldInsiders:     bps(st.PctInsiders),
		PctHeldInstitutions: bps(st.PctInstitutions),
		Week52High:          x100(pr.Week52High),
		Week52Low:           x100(pr.Week52Low),
		Week52Change:        x100(pr.Week52Change), // percentage -> bps
		Beta:                x100(pr.Beta),
		Day50Ma:             x100(pr.Day50MA),
		Day200Ma:            x100(pr.Day200MA),
		ForwardDivRate:      x100(dv.ForwardRate),
		ForwardDivYield:     bps(dv.ForwardYield),
		TrailingDivRate:     x100(dv.TrailingRate),
		TrailingDivYield:    bps(dv.TrailingYield),
		AvgDivYield5y:       x100(dv.AvgYield5y),
		PayoutRatio:         bps(dv.PayoutRatio),
		DividendFrequency:   dv.Frequency,
		DividendDate:        dv.DividendDate,
		ExDividendDate:      dv.ExDividendDate,
		LastSplitFactor:     dv.LastSplitFactor,
		LastSplitDate:       dv.LastSplitDate,
		FiscalYearEnds:      fin.FiscalYearEnds,
		MostRecentQuarter:   fin.MostRecentQuarter,
	}
}
