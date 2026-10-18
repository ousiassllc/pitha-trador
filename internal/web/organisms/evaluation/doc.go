// Package evaluation renders the realized-performance evaluation of issue
// #777 on the Performance page: the 合格ライン verdict, the metrics with
// sample sizes and confidence intervals (or 判定不能) by period, direction,
// exit reason, meta-labeling model and A/B arm, the cost breakdown, Jev's
// hit rate / Brier score and the meta model's OOS metrics beside the same
// trades' realized EV. It takes plain, already formatted props
// (internal/web/handler/performance maps internal/service/actuals onto
// them), so it stays decoupled from internal/service. It is its own package
// because internal/web/organisms is at its line limit.
package evaluation
