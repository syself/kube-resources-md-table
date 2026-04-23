package main

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

type options struct {
	allNamespaces          bool
	namespace              string
	kubeconfig             string
	threshold              float64
	lowThreshold           float64
	cpuLimitThreshold      float64
	memLimitThreshold      float64
	cpuRequestThreshold    float64
	memRequestThreshold    float64
	cpuLimitLowThreshold   float64
	memLimitLowThreshold   float64
	cpuRequestLowThreshold float64
	memRequestLowThreshold float64
}

type containerKey struct {
	namespace string
	pod       string
	container string
}

type containerUsage struct {
	cpuRaw string
	cpu    float64
	memRaw string
	mem    float64
}

type containerSpec struct {
	cpuRequestRaw string
	cpuRequest    float64
	cpuLimitRaw   string
	cpuLimit      float64
	memRequestRaw string
	memRequest    float64
	memLimitRaw   string
	memLimit      float64
}

type reportRow struct {
	namespace  string
	pod        string
	container  string
	cpuUse     string
	cpuRequest string
	cpuLimit   string
	cpuReqPct  float64
	cpuLimPct  float64
	memUse     string
	memRequest string
	memLimit   string
	memReqPct  float64
	memLimPct  float64
}

type viewConfig struct {
	title      string
	threshold  float64
	operator   string
	sortAsc    bool
	value      func(reportRow) float64
	headers    []string
	buildRow   func(reportRow) []string
	rightAlign map[int]bool
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	opts := options{
		threshold:              -1,
		lowThreshold:           -1,
		cpuLimitThreshold:      80,
		memLimitThreshold:      80,
		cpuRequestThreshold:    100,
		memRequestThreshold:    100,
		cpuLimitLowThreshold:   10,
		memLimitLowThreshold:   10,
		cpuRequestLowThreshold: 10,
		memRequestLowThreshold: 10,
		kubeconfig:             defaultKubeconfig(),
	}

	cmd := &cobra.Command{
		Use:           "pod-near-limit-report",
		Short:         "Render Markdown tables for pods nearing or exceeding CPU and memory thresholds",
		SilenceUsage:  true,
		SilenceErrors: true,
		Example: strings.Join([]string{
			"  pod-near-limit-report",
			"  pod-near-limit-report -n kube-system",
			"  pod-near-limit-report --threshold 90",
			"  pod-near-limit-report --low-threshold 15",
			"  pod-near-limit-report --mem-limit-threshold 75",
		}, "\n"),
		PreRun: func(cmd *cobra.Command, _ []string) {
			applyThresholdOverrides(cmd, &opts)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return run(cmd.Context(), opts)
		},
	}

	flags := cmd.Flags()
	flags.BoolVarP(&opts.allNamespaces, "all-namespaces", "A", false, "Query all namespaces (default when --namespace is not set)")
	flags.StringVarP(&opts.namespace, "namespace", "n", "", "Query one namespace")
	flags.StringVarP(&opts.kubeconfig, "kubeconfig", "k", opts.kubeconfig, "Use a specific kubeconfig")
	flags.Float64Var(&opts.threshold, "threshold", -1, "Set all four thresholds to the same percent")
	flags.Float64Var(&opts.lowThreshold, "low-threshold", -1, "Set all four low-usage thresholds to the same percent")
	flags.Float64Var(&opts.cpuLimitThreshold, "cpu-limit-threshold", 80, "CPU limit threshold percent")
	flags.Float64Var(&opts.memLimitThreshold, "mem-limit-threshold", 80, "Memory limit threshold percent")
	flags.Float64Var(&opts.cpuRequestThreshold, "cpu-request-threshold", 100, "CPU request threshold percent")
	flags.Float64Var(&opts.memRequestThreshold, "mem-request-threshold", 100, "Memory request threshold percent")
	flags.Float64Var(&opts.cpuLimitLowThreshold, "cpu-limit-low-threshold", 10, "CPU limit low-usage threshold percent")
	flags.Float64Var(&opts.memLimitLowThreshold, "mem-limit-low-threshold", 10, "Memory limit low-usage threshold percent")
	flags.Float64Var(&opts.cpuRequestLowThreshold, "cpu-request-low-threshold", 10, "CPU request low-usage threshold percent")
	flags.Float64Var(&opts.memRequestLowThreshold, "mem-request-low-threshold", 10, "Memory request low-usage threshold percent")

	return cmd
}

func applyThresholdOverrides(cmd *cobra.Command, opts *options) {
	if opts.threshold < 0 {
	} else {
		flags := cmd.Flags()
		if !flags.Changed("cpu-limit-threshold") {
			opts.cpuLimitThreshold = opts.threshold
		}
		if !flags.Changed("mem-limit-threshold") {
			opts.memLimitThreshold = opts.threshold
		}
		if !flags.Changed("cpu-request-threshold") {
			opts.cpuRequestThreshold = opts.threshold
		}
		if !flags.Changed("mem-request-threshold") {
			opts.memRequestThreshold = opts.threshold
		}
	}

	flags := cmd.Flags()
	if opts.lowThreshold < 0 {
		return
	}

	if !flags.Changed("cpu-limit-low-threshold") {
		opts.cpuLimitLowThreshold = opts.lowThreshold
	}
	if !flags.Changed("mem-limit-low-threshold") {
		opts.memLimitLowThreshold = opts.lowThreshold
	}
	if !flags.Changed("cpu-request-low-threshold") {
		opts.cpuRequestLowThreshold = opts.lowThreshold
	}
	if !flags.Changed("mem-request-low-threshold") {
		opts.memRequestLowThreshold = opts.lowThreshold
	}
}

func run(ctx context.Context, opts options) error {
	if err := validateThresholds(opts); err != nil {
		return err
	}

	cfg, err := buildConfig(opts.kubeconfig)
	if err != nil {
		return err
	}

	coreClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	metricsClient, err := metricsclient.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("create metrics client: %w", err)
	}

	namespace := effectiveNamespace(opts)

	pods, err := coreClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list pods: %w", err)
	}

	podMetrics, err := metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("collect live usage via metrics API: %w\nunable to collect live usage. Check metrics-server access", err)
	}

	rows := buildReportRows(pods.Items, podMetrics.Items)
	renderViews(os.Stdout, rows, opts)
	return nil
}

func validateThresholds(opts options) error {
	values := map[string]float64{
		"--cpu-limit-threshold":       opts.cpuLimitThreshold,
		"--mem-limit-threshold":       opts.memLimitThreshold,
		"--cpu-request-threshold":     opts.cpuRequestThreshold,
		"--mem-request-threshold":     opts.memRequestThreshold,
		"--cpu-limit-low-threshold":   opts.cpuLimitLowThreshold,
		"--mem-limit-low-threshold":   opts.memLimitLowThreshold,
		"--cpu-request-low-threshold": opts.cpuRequestLowThreshold,
		"--mem-request-low-threshold": opts.memRequestLowThreshold,
	}

	for name, value := range values {
		if value < 0 {
			return fmt.Errorf("%s must be >= 0", name)
		}
	}

	return nil
}

func buildConfig(kubeconfig string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		rules.ExplicitPath = kubeconfig
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		rules,
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}

	cfg.QPS = -1
	cfg.Burst = -1
	cfg.RateLimiter = nil
	return cfg, nil
}

func effectiveNamespace(opts options) string {
	if opts.allNamespaces || opts.namespace == "" {
		return metav1.NamespaceAll
	}
	return opts.namespace
}

func buildReportRows(pods []corev1.Pod, metrics []metricsv1beta1.PodMetrics) []reportRow {
	specs := make(map[containerKey]containerSpec, len(pods))
	for _, pod := range pods {
		for _, container := range pod.Spec.Containers {
			key := containerKey{
				namespace: pod.Namespace,
				pod:       pod.Name,
				container: container.Name,
			}
			specs[key] = containerSpec{
				cpuRequestRaw: quantityString(container.Resources.Requests, corev1.ResourceCPU),
				cpuRequest:    quantityMilli(container.Resources.Requests, corev1.ResourceCPU),
				cpuLimitRaw:   quantityString(container.Resources.Limits, corev1.ResourceCPU),
				cpuLimit:      quantityMilli(container.Resources.Limits, corev1.ResourceCPU),
				memRequestRaw: quantityString(container.Resources.Requests, corev1.ResourceMemory),
				memRequest:    quantityBytes(container.Resources.Requests, corev1.ResourceMemory),
				memLimitRaw:   quantityString(container.Resources.Limits, corev1.ResourceMemory),
				memLimit:      quantityBytes(container.Resources.Limits, corev1.ResourceMemory),
			}
		}
	}

	usages := make(map[containerKey]containerUsage, len(metrics))
	for _, podMetric := range metrics {
		for _, container := range podMetric.Containers {
			key := containerKey{
				namespace: podMetric.Namespace,
				pod:       podMetric.Name,
				container: container.Name,
			}
			usages[key] = containerUsage{
				cpuRaw: formatCPU(container.Usage[corev1.ResourceCPU]),
				cpu:    quantityMilli(container.Usage, corev1.ResourceCPU),
				memRaw: formatMemory(container.Usage[corev1.ResourceMemory]),
				mem:    quantityBytes(container.Usage, corev1.ResourceMemory),
			}
		}
	}

	rows := make([]reportRow, 0, len(specs))
	for key, spec := range specs {
		usage, ok := usages[key]
		if !ok {
			usage = containerUsage{
				cpuRaw: "-",
				cpu:    -1,
				memRaw: "-",
				mem:    -1,
			}
		}

		row := reportRow{
			namespace:  key.namespace,
			pod:        key.pod,
			container:  key.container,
			cpuUse:     usage.cpuRaw,
			cpuRequest: spec.cpuRequestRaw,
			cpuLimit:   spec.cpuLimitRaw,
			cpuReqPct:  percent(usage.cpu, spec.cpuRequest),
			cpuLimPct:  percent(usage.cpu, spec.cpuLimit),
			memUse:     usage.memRaw,
			memRequest: spec.memRequestRaw,
			memLimit:   spec.memLimitRaw,
			memReqPct:  percent(usage.mem, spec.memRequest),
			memLimPct:  percent(usage.mem, spec.memLimit),
		}

		if maxMetric(row) < 0 {
			continue
		}

		rows = append(rows, row)
	}

	return rows
}

func renderViews(out io.Writer, rows []reportRow, opts options) {
	views := []viewConfig{
		{
			title:     "reaching cpu-limit",
			threshold: opts.cpuLimitThreshold,
			operator:  ">=",
			sortAsc:   false,
			value: func(row reportRow) float64 {
				return row.cpuLimPct
			},
			headers: []string{"namespace", "pod", "container", "cpu_use", "cpu_limit", "cpu_lim_pct"},
			buildRow: func(row reportRow) []string {
				return []string{row.namespace, row.pod, row.container, row.cpuUse, row.cpuLimit, percentText(row.cpuLimPct)}
			},
			rightAlign: map[int]bool{3: true, 4: true, 5: true},
		},
		{
			title:     "reaching mem-limit",
			threshold: opts.memLimitThreshold,
			operator:  ">=",
			sortAsc:   false,
			value: func(row reportRow) float64 {
				return row.memLimPct
			},
			headers: []string{"namespace", "pod", "container", "mem_use", "mem_limit", "mem_lim_pct"},
			buildRow: func(row reportRow) []string {
				return []string{row.namespace, row.pod, row.container, row.memUse, row.memLimit, percentText(row.memLimPct)}
			},
			rightAlign: map[int]bool{3: true, 4: true, 5: true},
		},
		{
			title:     "above cpu-request",
			threshold: opts.cpuRequestThreshold,
			operator:  ">",
			sortAsc:   false,
			value: func(row reportRow) float64 {
				return row.cpuReqPct
			},
			headers: []string{"namespace", "pod", "container", "cpu_use", "cpu_request", "cpu_req_pct"},
			buildRow: func(row reportRow) []string {
				return []string{row.namespace, row.pod, row.container, row.cpuUse, row.cpuRequest, percentText(row.cpuReqPct)}
			},
			rightAlign: map[int]bool{3: true, 4: true, 5: true},
		},
		{
			title:     "above mem-request",
			threshold: opts.memRequestThreshold,
			operator:  ">",
			sortAsc:   false,
			value: func(row reportRow) float64 {
				return row.memReqPct
			},
			headers: []string{"namespace", "pod", "container", "mem_use", "mem_request", "mem_req_pct"},
			buildRow: func(row reportRow) []string {
				return []string{row.namespace, row.pod, row.container, row.memUse, row.memRequest, percentText(row.memReqPct)}
			},
			rightAlign: map[int]bool{3: true, 4: true, 5: true},
		},
	}

	for _, view := range views {
		renderView(out, rows, view)
	}

	renderLowUsageView(out, rows,
		"well below cpu request/limit",
		opts.cpuRequestLowThreshold,
		opts.cpuLimitLowThreshold,
		func(row reportRow) float64 { return row.cpuReqPct },
		func(row reportRow) float64 { return row.cpuLimPct },
		[]string{"namespace", "pod", "container", "cpu_use", "cpu_request", "cpu_req_pct", "cpu_limit", "cpu_lim_pct"},
		func(row reportRow) []string {
			return []string{
				row.namespace,
				row.pod,
				row.container,
				row.cpuUse,
				row.cpuRequest,
				percentText(row.cpuReqPct),
				row.cpuLimit,
				percentText(row.cpuLimPct),
			}
		},
		map[int]bool{3: true, 4: true, 5: true, 6: true, 7: true},
	)
	renderLowUsageView(out, rows,
		"well below mem request/limit",
		opts.memRequestLowThreshold,
		opts.memLimitLowThreshold,
		func(row reportRow) float64 { return row.memReqPct },
		func(row reportRow) float64 { return row.memLimPct },
		[]string{"namespace", "pod", "container", "mem_use", "mem_request", "mem_req_pct", "mem_limit", "mem_lim_pct"},
		func(row reportRow) []string {
			return []string{
				row.namespace,
				row.pod,
				row.container,
				row.memUse,
				row.memRequest,
				percentText(row.memReqPct),
				row.memLimit,
				percentText(row.memLimPct),
			}
		},
		map[int]bool{3: true, 4: true, 5: true, 6: true, 7: true},
	)
}

func defaultKubeconfig() string {
	candidates := []string{}

	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "prod-readonly.kubeconfig"))
	}

	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Join(filepath.Dir(file), "..", "prod-readonly.kubeconfig"))
	}

	for _, candidate := range candidates {
		if candidate != "" && fileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func quantityString(list corev1.ResourceList, name corev1.ResourceName) string {
	quantity, ok := list[name]
	if !ok {
		return "-"
	}
	return quantity.String()
}

func quantityMilli(list corev1.ResourceList, name corev1.ResourceName) float64 {
	quantity, ok := list[name]
	if !ok {
		return -1
	}
	return float64(quantity.MilliValue())
}

func quantityBytes(list corev1.ResourceList, name corev1.ResourceName) float64 {
	quantity, ok := list[name]
	if !ok {
		return -1
	}
	return float64(quantity.Value())
}

func formatCPU(q resource.Quantity) string {
	if q.IsZero() {
		return "0"
	}

	if q.MilliValue()%1000 == 0 {
		return fmt.Sprintf("%d", q.MilliValue()/1000)
	}

	return fmt.Sprintf("%dm", q.MilliValue())
}

func formatMemory(q resource.Quantity) string {
	value := q.Value()
	if value == 0 {
		return "0"
	}

	type unit struct {
		suffix string
		size   float64
	}

	units := []unit{
		{suffix: "Ei", size: math.Pow(1024, 6)},
		{suffix: "Pi", size: math.Pow(1024, 5)},
		{suffix: "Ti", size: math.Pow(1024, 4)},
		{suffix: "Gi", size: math.Pow(1024, 3)},
		{suffix: "Mi", size: math.Pow(1024, 2)},
		{suffix: "Ki", size: 1024},
	}

	bytes := float64(value)
	for _, unit := range units {
		if bytes >= unit.size {
			amount := bytes / unit.size
			if amount >= 10 || math.Mod(amount, 1) == 0 {
				return fmt.Sprintf("%.0f%s", amount, unit.suffix)
			}
			return fmt.Sprintf("%.1f%s", amount, unit.suffix)
		}
	}

	return fmt.Sprintf("%d", value)
}

func percent(used, target float64) float64 {
	if used < 0 || target <= 0 {
		return -1
	}
	return used / target * 100
}

func percentText(value float64) string {
	if value < 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", value)
}

func maxMetric(row reportRow) float64 {
	best := -1.0
	for _, value := range []float64{row.cpuReqPct, row.cpuLimPct, row.memReqPct, row.memLimPct} {
		if value > best {
			best = value
		}
	}
	return best
}

func renderView(out io.Writer, rows []reportRow, view viewConfig) {
	fmt.Fprintf(out, "## %s\n\n", view.title)
	fmt.Fprintf(out, "Threshold: `%s %s%%`\n\n", view.operator, formatThreshold(view.threshold))

	filtered := make([]reportRow, 0)
	for _, row := range rows {
		value := view.value(row)
		if value < 0 {
			continue
		}
		if view.operator == ">" && value <= view.threshold {
			continue
		}
		if view.operator == ">=" && value < view.threshold {
			continue
		}
		if view.operator == "<=" && value > view.threshold {
			continue
		}
		filtered = append(filtered, row)
	}

	if len(filtered) == 0 {
		fmt.Fprintln(out, "(none)")
		fmt.Fprintln(out)
		return
	}

	sort.Slice(filtered, func(i, j int) bool {
		left := view.value(filtered[i])
		right := view.value(filtered[j])
		if left != right {
			if view.sortAsc {
				return left < right
			}
			return left > right
		}
		if filtered[i].namespace != filtered[j].namespace {
			return filtered[i].namespace < filtered[j].namespace
		}
		if filtered[i].pod != filtered[j].pod {
			return filtered[i].pod < filtered[j].pod
		}
		return filtered[i].container < filtered[j].container
	})

	tableRows := make([][]string, 0, len(filtered)+1)
	tableRows = append(tableRows, view.headers)
	for _, row := range filtered {
		tableRows = append(tableRows, view.buildRow(row))
	}

	writeMarkdownTable(out, tableRows, view.rightAlign)
	fmt.Fprintln(out)
}

func renderLowUsageView(
	out io.Writer,
	rows []reportRow,
	title string,
	requestThreshold float64,
	limitThreshold float64,
	requestValue func(reportRow) float64,
	limitValue func(reportRow) float64,
	headers []string,
	buildRow func(reportRow) []string,
	rightAlign map[int]bool,
) {
	fmt.Fprintf(out, "## %s\n\n", title)
	fmt.Fprintf(
		out,
		"Threshold: `request <= %s%% or limit <= %s%%`\n\n",
		formatThreshold(requestThreshold),
		formatThreshold(limitThreshold),
	)

	filtered := make([]reportRow, 0)
	for _, row := range rows {
		requestPct := requestValue(row)
		limitPct := limitValue(row)
		if !matchesLowThreshold(requestPct, requestThreshold) && !matchesLowThreshold(limitPct, limitThreshold) {
			continue
		}
		filtered = append(filtered, row)
	}

	if len(filtered) == 0 {
		fmt.Fprintln(out, "(none)")
		fmt.Fprintln(out)
		return
	}

	sort.Slice(filtered, func(i, j int) bool {
		left := lowSortValue(requestValue(filtered[i]), requestThreshold, limitValue(filtered[i]), limitThreshold)
		right := lowSortValue(requestValue(filtered[j]), requestThreshold, limitValue(filtered[j]), limitThreshold)
		if left != right {
			return left < right
		}
		if filtered[i].namespace != filtered[j].namespace {
			return filtered[i].namespace < filtered[j].namespace
		}
		if filtered[i].pod != filtered[j].pod {
			return filtered[i].pod < filtered[j].pod
		}
		return filtered[i].container < filtered[j].container
	})

	tableRows := make([][]string, 0, len(filtered)+1)
	tableRows = append(tableRows, headers)
	for _, row := range filtered {
		tableRows = append(tableRows, buildRow(row))
	}

	writeMarkdownTable(out, tableRows, rightAlign)
	fmt.Fprintln(out)
}

func matchesLowThreshold(value, threshold float64) bool {
	return value >= 0 && value <= threshold
}

func lowSortValue(requestValue, requestThreshold, limitValue, limitThreshold float64) float64 {
	best := math.MaxFloat64
	if matchesLowThreshold(requestValue, requestThreshold) {
		best = requestValue
	}
	if matchesLowThreshold(limitValue, limitThreshold) && limitValue < best {
		best = limitValue
	}
	return best
}

func writeMarkdownTable(out io.Writer, rows [][]string, rightAlign map[int]bool) {
	if len(rows) == 0 {
		return
	}

	widths := make([]int, len(rows[0]))
	escaped := make([][]string, len(rows))
	for r, row := range rows {
		escaped[r] = make([]string, len(row))
		for c, cell := range row {
			cell = strings.ReplaceAll(cell, "|", "\\|")
			escaped[r][c] = cell
			if len(cell) > widths[c] {
				widths[c] = len(cell)
			}
		}
	}

	for r, row := range escaped {
		fmt.Fprint(out, "|")
		for c, cell := range row {
			if rightAlign[c] && r > 0 {
				fmt.Fprintf(out, " %*s |", widths[c], cell)
				continue
			}
			fmt.Fprintf(out, " %-*s |", widths[c], cell)
		}
		fmt.Fprintln(out)

		if r == 0 {
			fmt.Fprint(out, "|")
			for c := range row {
				if rightAlign[c] {
					fmt.Fprintf(out, " %s: |", strings.Repeat("-", max(widths[c]-1, 1)))
					continue
				}
				fmt.Fprintf(out, " %s |", strings.Repeat("-", max(widths[c], 1)))
			}
			fmt.Fprintln(out)
		}
	}
}

func formatThreshold(value float64) string {
	if math.Mod(value, 1) == 0 {
		return fmt.Sprintf("%.0f", value)
	}
	return fmt.Sprintf("%.1f", value)
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
