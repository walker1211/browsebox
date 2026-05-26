package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/walker1211/browsebox/internal/browser"
	"github.com/walker1211/browsebox/internal/mihomo"
	"github.com/walker1211/browsebox/internal/state"
)

const (
	managedByBrowsebox      = "browsebox"
	controllerLookupTimeout = 3 * time.Second
	controllerReadyTimeout  = 10 * time.Second
	controllerReadyInterval = 25 * time.Millisecond
	selectNodeTimeout       = 2 * time.Second
	processStopTimeout      = 2 * time.Second
)

type process interface {
	PID() int
	Signal(os.Signal) error
	Kill() error
	Wait() error
}

type cmdProcess struct {
	cmd *exec.Cmd
}

type osProcess struct {
	process *os.Process
}

type processInfo struct {
	PID     int
	Owner   string
	Command string
}

func (p cmdProcess) PID() int {
	if p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}

func (p cmdProcess) Signal(sig os.Signal) error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Signal(sig)
}

func (p cmdProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

func (p cmdProcess) Wait() error {
	return p.cmd.Wait()
}

func (p osProcess) PID() int {
	if p.process == nil {
		return 0
	}
	return p.process.Pid
}

func (p osProcess) Signal(sig os.Signal) error {
	if p.process == nil {
		return nil
	}
	return p.process.Signal(sig)
}

func (p osProcess) Kill() error {
	if p.process == nil {
		return nil
	}
	return p.process.Kill()
}

func (p osProcess) Wait() error {
	if p.process == nil {
		return nil
	}
	_, err := p.process.Wait()
	return err
}

var (
	readFile           = os.ReadFile
	writeRuntimeConfig = mihomo.WriteRuntimeConfig
	startMihomoProcess = func(ctx context.Context, binaryPath, runtimeDir, configPath string) (process, error) {
		cmd, err := mihomo.StartProcess(ctx, binaryPath, runtimeDir, configPath)
		if err != nil {
			return nil, err
		}
		return cmdProcess{cmd: cmd}, nil
	}
	startChrome = func(ctx context.Context, chromePath string, opts browser.Options) (process, error) {
		cmd, err := browser.StartChrome(ctx, chromePath, opts)
		if err != nil {
			return nil, err
		}
		return cmdProcess{cmd: cmd}, nil
	}
	findProcess = func(pid int) (process, error) {
		p, err := os.FindProcess(pid)
		if err != nil {
			return nil, err
		}
		return osProcess{process: p}, nil
	}
	inspectProcess = func(pid int) (processInfo, error) {
		cmd := exec.Command("ps", "-ww", "-p", strconv.Itoa(pid), "-o", "uid=", "-o", "command=")
		out, err := cmd.Output()
		line := strings.TrimSpace(string(out))
		if err != nil || line == "" {
			return processInfo{}, os.ErrNotExist
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return processInfo{}, fmt.Errorf("process %d inspection missing owner or command", pid)
		}
		return processInfo{PID: pid, Owner: fields[0], Command: strings.TrimSpace(strings.TrimPrefix(line, fields[0]))}, nil
	}
	signalProcess       = defaultSignalProcess
	processAlive        = defaultProcessAlive
	checkLocalPorts     = ensureLocalPortsAvailable
	currentProcessOwner = func() (string, error) {
		return strconv.Itoa(os.Geteuid()), nil
	}
)

// App executes browsebox commands without owning CLI parsing concerns.
type App struct {
	stdout io.Writer
	stderr io.Writer
}

// New creates an App that writes user-facing output to stdout and stderr.
func New(stdout, stderr io.Writer) *App {
	return &App{
		stdout: stdout,
		stderr: stderr,
	}
}

func controllerClient(opts Options) (*mihomo.Client, error) {
	if opts.ControllerURL != "" {
		if err := validateLocalControllerURL(opts.ControllerURL); err != nil {
			return nil, err
		}
		return mihomo.NewTCPClient(opts.ControllerURL), nil
	}
	if opts.ControllerSocket != "" {
		return mihomo.NewClient(opts.ControllerSocket), nil
	}
	if opts.ControllerPipe != "" {
		return mihomo.NewPipeClient(opts.ControllerPipe), nil
	}
	return mihomo.NewClient(opts.ControllerSocket), nil
}

func validateLocalControllerURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("controller-url must be an http://localhost URL")
	}
	if parsed.Port() == "" && hasExplicitControllerPort(parsed.Host) {
		return fmt.Errorf("controller-url port must be numeric")
	}
	if port := parsed.Port(); port != "" {
		portNumber, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("controller-url port must be numeric")
		}
		if portNumber <= 0 || portNumber > 65535 {
			return fmt.Errorf("controller-url port must be between 1 and 65535")
		}
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("controller-url must point to localhost")
	}
	return nil
}

func hasExplicitControllerPort(host string) bool {
	if strings.HasPrefix(host, "[") {
		closingBracket := strings.LastIndex(host, "]")
		return closingBracket >= 0 && len(host) > closingBracket+1 && host[closingBracket+1] == ':'
	}
	return strings.Count(host, ":") == 1
}

// Groups lists available proxy groups.
func (a *App) Groups(ctx context.Context, opts Options) error {
	client, err := controllerClient(opts)
	if err != nil {
		return err
	}
	groupCtx, cancelGroup := context.WithTimeout(ctx, controllerLookupTimeout)
	groups, err := client.ProxyGroups(groupCtx)
	cancelGroup()
	if err != nil {
		return fmt.Errorf("list proxy groups: %w", err)
	}

	writer := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "GROUP\tTYPE\tNODES"); err != nil {
		return err
	}
	for _, group := range groups {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%d\n", sanitizeDisplayName(group.Name), sanitizeDisplayName(group.Type), len(group.All)); err != nil {
			return err
		}
	}
	return writer.Flush()
}

// Nodes lists available proxy nodes.
func (a *App) Nodes(ctx context.Context, opts Options) error {
	if err := validateNodeTuning(opts); err != nil {
		return err
	}

	client, err := controllerClient(opts)
	if err != nil {
		return err
	}
	groupName, group, err := resolveEffectiveProxyGroup(ctx, client, opts.Group)
	if err != nil {
		return err
	}

	targetURLs := nodeProbeURLs(opts)
	results := collectNodeDelays(ctx, client, group.All, targetURLs, opts.NodeProbeRounds, opts.NodeProbeIntervalMS, opts.NodesConcurrency, opts.DelayTimeoutMS)
	sortNodeDelayResults(results)
	displayResults := filterNodeDelayResults(results, opts.ShowUnhealthyNodes)
	outputOptions := nodeDelayOutputOptions{
		total:            len(results),
		healthy:          countHealthyNodeDelayResults(results),
		currentNode:      group.Now,
		highlightCurrent: opts.HighlightCurrentNode,
	}

	if err := writeNodeDelayResults(a.stdout, displayResults, outputOptions); err != nil {
		return err
	}
	if !opts.SelectFastest {
		return nil
	}
	return a.selectFastestNode(ctx, client, groupName, results)
}

func (a *App) selectFastestNode(ctx context.Context, client *mihomo.Client, group string, results []nodeDelayResult) error {
	for _, result := range results {
		if !result.healthy {
			continue
		}
		selectCtx, cancelSelect := context.WithTimeout(ctx, selectNodeTimeout)
		err := client.SelectNode(selectCtx, group, result.name)
		cancelSelect()
		if err != nil {
			return fmt.Errorf("select fastest node %q in group %q: %w", result.name, group, err)
		}
		_, err = fmt.Fprintf(a.stdout, "selected %s (%dms) for group %s\n", sanitizeDisplayName(result.name), result.delay, sanitizeDisplayName(group))
		return err
	}
	return fmt.Errorf("select fastest node in group %q: no healthy nodes", group)
}

type nodeDelayResult struct {
	index   int
	name    string
	delay   int
	healthy bool
}

func validateNodeTuning(opts Options) error {
	if opts.NodesConcurrency <= 0 {
		return errors.New("--nodes-concurrency must be a positive integer")
	}
	if opts.NodeProbeRounds <= 0 {
		return errors.New("--probe-rounds must be a positive integer")
	}
	if opts.NodeProbeIntervalMS < 0 {
		return errors.New("--probe-interval-ms must be zero or a positive integer")
	}
	return validateDelayTimeout(opts.DelayTimeoutMS)
}

func validateDelayTimeout(timeoutMS int) error {
	if timeoutMS <= 0 {
		return errors.New("--delay-timeout-ms must be a positive integer")
	}
	return nil
}

func resolveEffectiveProxyGroup(ctx context.Context, client *mihomo.Client, name string) (string, mihomo.ProxyGroupInfo, error) {
	groupName := strings.TrimSpace(name)
	if groupName == "" {
		resolvedGroupName, err := autoProxyGroupName(ctx, client)
		if err != nil {
			return "", mihomo.ProxyGroupInfo{}, fmt.Errorf("auto resolve proxy group: %w", err)
		}
		groupName = resolvedGroupName
	}

	group, err := lookupProxyGroup(ctx, client, groupName)
	if err == nil {
		return groupName, group, nil
	}

	originalLookupErr := err
	resolvedGroupName, resolveErr := resolveProxyGroupName(ctx, client, groupName)
	if resolveErr == nil && resolvedGroupName != groupName {
		groupName = resolvedGroupName
		group, err = lookupProxyGroup(ctx, client, groupName)
		if err == nil {
			return groupName, group, nil
		}
	} else {
		var ambiguousErr *ambiguousProxyGroupError
		if errors.As(resolveErr, &ambiguousErr) {
			return "", mihomo.ProxyGroupInfo{}, ambiguousErr
		}
		err = originalLookupErr
	}
	return "", mihomo.ProxyGroupInfo{}, fmt.Errorf("lookup proxy group %q: %w", groupName, err)
}

func lookupProxyGroup(ctx context.Context, client *mihomo.Client, name string) (mihomo.ProxyGroupInfo, error) {
	groupCtx, cancelGroup := context.WithTimeout(ctx, controllerLookupTimeout)
	group, err := client.ProxyGroup(groupCtx, name)
	cancelGroup()
	return group, err
}

type ambiguousProxyGroupError struct {
	name    string
	matches []string
}

func (e *ambiguousProxyGroupError) Error() string {
	return fmt.Sprintf("proxy group %q is ambiguous: matches %s", e.name, strings.Join(e.matches, ", "))
}

func resolveProxyGroupName(ctx context.Context, client *mihomo.Client, name string) (string, error) {
	groupCtx, cancelGroup := context.WithTimeout(ctx, controllerLookupTimeout)
	groups, err := client.ProxyGroups(groupCtx)
	cancelGroup()
	if err != nil {
		return "", fmt.Errorf("list proxy groups: %w", err)
	}

	for _, group := range groups {
		if group.Name == name {
			return group.Name, nil
		}
	}

	var matches []string
	for _, group := range groups {
		if strings.HasSuffix(group.Name, name) {
			matches = append(matches, group.Name)
		}
	}
	sort.Strings(matches)
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", &ambiguousProxyGroupError{name: name, matches: matches}
	}
	return name, nil
}

func autoProxyGroupName(ctx context.Context, client *mihomo.Client) (string, error) {
	groupCtx, cancelGroup := context.WithTimeout(ctx, controllerLookupTimeout)
	groups, err := client.ProxyGroups(groupCtx)
	cancelGroup()
	if err != nil {
		return "", fmt.Errorf("list proxy groups: %w", err)
	}

	byName := make(map[string]mihomo.ProxyGroupInfo, len(groups))
	for _, group := range groups {
		byName[group.Name] = group
	}
	if globalGroup, ok := byName["GLOBAL"]; ok && selectsProxyGroup(globalGroup, byName) {
		if groupName, ok, err := selectedLeafProxyGroupName(globalGroup, byName); ok || err != nil {
			return groupName, err
		}
	}

	selectedGroups := map[string]struct{}{}
	for _, group := range groups {
		selectedName := strings.TrimSpace(group.Now)
		if selectedName == "" || selectedName == group.Name {
			continue
		}
		selectedGroup, ok := byName[selectedName]
		if !ok || len(selectedGroup.All) == 0 {
			continue
		}
		if _, ok := byName[strings.TrimSpace(selectedGroup.Now)]; ok {
			continue
		}
		if !isAutoProxyGroupCandidate(selectedGroup) {
			continue
		}
		selectedGroups[selectedGroup.Name] = struct{}{}
	}
	if name, ok, err := singleProxyGroupName(selectedGroups); ok || err != nil {
		return name, err
	}

	currentGroups := map[string]struct{}{}
	for _, group := range groups {
		selectedName := strings.TrimSpace(group.Now)
		if selectedName == "" {
			continue
		}
		if _, ok := byName[selectedName]; ok {
			continue
		}
		if !isAutoProxyGroupCandidate(group) {
			continue
		}
		currentGroups[group.Name] = struct{}{}
	}
	if name, ok, err := singleProxyGroupName(currentGroups); ok || err != nil {
		return name, err
	}

	return "", errors.New("no current proxy group found; set session.group or pass --group")
}

func selectsProxyGroup(group mihomo.ProxyGroupInfo, byName map[string]mihomo.ProxyGroupInfo) bool {
	selectedName := strings.TrimSpace(group.Now)
	selectedGroup, ok := byName[selectedName]
	return ok && len(selectedGroup.All) > 0
}

func isAutoProxyGroupCandidate(group mihomo.ProxyGroupInfo) bool {
	if group.Name == "GLOBAL" {
		return false
	}
	typeName := strings.ToLower(strings.TrimSpace(group.Type))
	return typeName == "" || typeName == "selector"
}

func selectedLeafProxyGroupName(group mihomo.ProxyGroupInfo, byName map[string]mihomo.ProxyGroupInfo) (string, bool, error) {
	seen := map[string]struct{}{group.Name: {}}
	current := group
	for {
		selectedName := strings.TrimSpace(current.Now)
		if selectedName == "" {
			return "", false, nil
		}
		selectedGroup, ok := byName[selectedName]
		if !ok || len(selectedGroup.All) == 0 {
			return current.Name, true, nil
		}
		if _, ok := seen[selectedGroup.Name]; ok {
			return "", false, fmt.Errorf("proxy group selection cycle at %q", selectedGroup.Name)
		}
		seen[selectedGroup.Name] = struct{}{}
		current = selectedGroup
	}
}

func singleProxyGroupName(names map[string]struct{}) (string, bool, error) {
	if len(names) == 0 {
		return "", false, nil
	}
	matches := make([]string, 0, len(names))
	for name := range names {
		matches = append(matches, name)
	}
	sort.Strings(matches)
	if len(matches) > 1 {
		return "", true, &ambiguousProxyGroupError{name: "auto", matches: matches}
	}
	return matches[0], true, nil
}

func nodeProbeURLs(opts Options) []string {
	var urls []string
	for _, healthURL := range opts.HealthURLs {
		trimmedURL := strings.TrimSpace(healthURL)
		if trimmedURL != "" {
			urls = append(urls, trimmedURL)
		}
	}
	if len(urls) == 0 {
		urls = append(urls, strings.TrimSpace(opts.TargetURL))
	}
	return urls
}

func collectNodeDelays(ctx context.Context, client *mihomo.Client, nodes []string, targetURLs []string, probeRounds, probeIntervalMS, concurrency, timeoutMS int) []nodeDelayResult {
	results := make([]nodeDelayResult, len(nodes))
	if len(nodes) == 0 {
		return results
	}

	workerCount := min(concurrency, len(nodes))

	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = checkNodeDelay(ctx, client, index, nodes[index], targetURLs, probeRounds, probeIntervalMS, timeoutMS)
			}
		}()
	}

	for index := range nodes {
		jobs <- index
	}
	close(jobs)
	wg.Wait()

	return results
}

func checkNodeDelay(ctx context.Context, client *mihomo.Client, index int, node string, targetURLs []string, probeRounds, probeIntervalMS, timeoutMS int) nodeDelayResult {
	result := nodeDelayResult{index: index, name: node}
	var totalDelay int
	var probeCount int
	for round := range probeRounds {
		if round > 0 && probeIntervalMS > 0 {
			interval := time.NewTimer(time.Duration(probeIntervalMS) * time.Millisecond)
			select {
			case <-ctx.Done():
				interval.Stop()
				return result
			case <-interval.C:
			}
		}
		for _, targetURL := range targetURLs {
			delayCtx, cancelDelay := context.WithTimeout(ctx, nodeDelayContextTimeout(timeoutMS))
			delay, err := client.Delay(delayCtx, node, targetURL, timeoutMS)
			cancelDelay()
			if err != nil || delay.Error != "" {
				return result
			}
			totalDelay += delay.Delay
			probeCount++
		}
	}
	if probeCount == 0 {
		return result
	}
	result.delay = totalDelay / probeCount
	result.healthy = true
	return result
}

func nodeDelayContextTimeout(timeoutMS int) time.Duration {
	return time.Duration(timeoutMS)*time.Millisecond + time.Second
}

func sortNodeDelayResults(results []nodeDelayResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].healthy != results[j].healthy {
			return results[i].healthy
		}
		if results[i].healthy && results[i].delay != results[j].delay {
			return results[i].delay < results[j].delay
		}
		return results[i].index < results[j].index
	})
}

func filterNodeDelayResults(results []nodeDelayResult, showUnhealthy bool) []nodeDelayResult {
	if showUnhealthy {
		return results
	}
	filtered := make([]nodeDelayResult, 0, len(results))
	for _, result := range results {
		if result.healthy {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func countHealthyNodeDelayResults(results []nodeDelayResult) int {
	healthy := 0
	for _, result := range results {
		if result.healthy {
			healthy++
		}
	}
	return healthy
}

const (
	currentNodeRowColor = "\x1b[1;36m"
	ansiReset           = "\x1b[0m"
)

type nodeDelayOutputOptions struct {
	total            int
	healthy          int
	currentNode      string
	highlightCurrent bool
}

type nodeDelayRow struct {
	node      string
	status    string
	delay     string
	highlight bool
}

func writeNodeDelayResults(w io.Writer, results []nodeDelayResult, options nodeDelayOutputOptions) error {
	if _, err := fmt.Fprintf(w, "nodes: total %d, ok %d, shown %d\n", options.total, options.healthy, len(results)); err != nil {
		return err
	}
	rows := make([]nodeDelayRow, 0, len(results)+1)
	rows = append(rows, nodeDelayRow{node: "NODE", status: "STATUS", delay: "DELAY"})
	for _, result := range results {
		rows = append(rows, nodeDelayResultRow(result, options.currentNode, options.highlightCurrent))
	}
	return writeFixedWidthRows(w, rows)
}

func nodeDelayResultRow(result nodeDelayResult, currentNode string, highlightCurrent bool) nodeDelayRow {
	row := nodeDelayRow{
		node:      sanitizeDisplayName(result.name),
		highlight: highlightCurrent && currentNode != "" && result.name == currentNode,
	}
	if !result.healthy {
		row.status = "unhealthy"
		row.delay = "-"
		return row
	}
	row.status = "ok"
	row.delay = fmt.Sprintf("%dms", result.delay)
	return row
}

func writeFixedWidthRows(w io.Writer, rows []nodeDelayRow) error {
	nodeWidth := 0
	statusWidth := 0
	for _, row := range rows {
		nodeWidth = max(nodeWidth, displayWidth(row.node))
		statusWidth = max(statusWidth, displayWidth(row.status))
	}
	for _, row := range rows {
		line := fmt.Sprintf(
			"%s%s  %s%s  %s",
			row.node,
			spacesForDisplayWidth(nodeWidth-displayWidth(row.node)),
			row.status,
			spacesForDisplayWidth(statusWidth-displayWidth(row.status)),
			row.delay,
		)
		if row.highlight {
			line = currentNodeRowColor + line + ansiReset
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func spacesForDisplayWidth(width int) string {
	if width <= 0 {
		return ""
	}
	return strings.Repeat(" ", width)
}

func displayWidth(text string) int {
	width := 0
	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if unicode.Is(unicode.Mn, r) || r == '‍' || isVariationSelector(r) || isEmojiModifier(r) {
			continue
		}
		if isRegionalIndicator(r) && i+1 < len(runes) && isRegionalIndicator(runes[i+1]) {
			width += 2
			i++
			continue
		}
		if isEmojiSequenceStart(runes, i) {
			width += 2
			i = skipEmojiSequence(runes, i)
			continue
		}
		if isWideRune(r) {
			width += 2
			continue
		}
		width++
	}
	return width
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F:
		return true
	case r >= 0x2329 && r <= 0x232A:
		return true
	case r >= 0x2E80 && r <= 0xA4CF:
		return true
	case r >= 0xAC00 && r <= 0xD7A3:
		return true
	case r >= 0xF900 && r <= 0xFAFF:
		return true
	case r >= 0xFE10 && r <= 0xFE19:
		return true
	case r >= 0xFE30 && r <= 0xFE6F:
		return true
	case r >= 0xFF00 && r <= 0xFF60:
		return true
	case r >= 0xFFE0 && r <= 0xFFE6:
		return true
	case isWideSymbolRune(r):
		return true
	case r >= 0x1F300 && r <= 0x1FAFF:
		return true
	default:
		return false
	}
}

func isWideSymbolRune(r rune) bool {
	switch {
	case r >= 0x2614 && r <= 0x2615:
		return true
	case r >= 0x2630 && r <= 0x2637:
		return true
	case r >= 0x2648 && r <= 0x2653:
		return true
	case r == 0x267F:
		return true
	case r >= 0x268A && r <= 0x268F:
		return true
	case r == 0x2693 || r == 0x26A1:
		return true
	case r >= 0x26AA && r <= 0x26AB:
		return true
	case r >= 0x26BD && r <= 0x26BE:
		return true
	case r >= 0x26C4 && r <= 0x26C5:
		return true
	case r == 0x26CE || r == 0x26D4 || r == 0x26EA:
		return true
	case r >= 0x26F2 && r <= 0x26F3:
		return true
	case r == 0x26F5 || r == 0x26FA || r == 0x26FD:
		return true
	case r == 0x2705:
		return true
	case r >= 0x270A && r <= 0x270B:
		return true
	case r == 0x2728 || r == 0x274C || r == 0x274E:
		return true
	case r >= 0x2753 && r <= 0x2755:
		return true
	case r == 0x2757:
		return true
	case r >= 0x2795 && r <= 0x2797:
		return true
	case r == 0x27B0 || r == 0x27BF:
		return true
	default:
		return false
	}
}

func isEmojiSequenceStart(runes []rune, index int) bool {
	if isEmojiRune(runes[index]) {
		return true
	}
	return isEmojiVariationBase(runes[index]) && index+1 < len(runes) && runes[index+1] == 0xFE0F
}

func skipEmojiSequence(runes []rune, index int) int {
	index = skipEmojiMarks(runes, index)
	for index+2 < len(runes) && runes[index+1] == '‍' && isEmojiSequenceStart(runes, index+2) {
		index = skipEmojiMarks(runes, index+2)
	}
	return index
}

func skipEmojiMarks(runes []rune, index int) int {
	for index+1 < len(runes) && (isVariationSelector(runes[index+1]) || isEmojiModifier(runes[index+1])) {
		index++
	}
	return index
}

func isEmojiRune(r rune) bool {
	return r >= 0x1F300 && r <= 0x1FAFF
}

func isEmojiVariationBase(r rune) bool {
	return r >= 0x2600 && r <= 0x27BF
}

func isEmojiModifier(r rune) bool {
	return r >= 0x1F3FB && r <= 0x1F3FF
}

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

func isVariationSelector(r rune) bool {
	return r >= 0xFE00 && r <= 0xFE0F
}

func resolveStartupProxyGroup(ctx context.Context, opts *Options) error {
	if strings.TrimSpace(opts.Group) != "" {
		opts.Group = strings.TrimSpace(opts.Group)
		return nil
	}
	client, err := controllerClient(*opts)
	if err != nil {
		return err
	}
	groupName, _, err := resolveEffectiveProxyGroup(ctx, client, opts.Group)
	if err != nil {
		return err
	}
	opts.Group = groupName
	return nil
}

// Run launches a temporary isolated browser session.
func (a *App) Run(ctx context.Context, opts Options) error {
	if err := requireNode(opts); err != nil {
		return err
	}

	signalCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	childCtx, cancelChildren := context.WithCancel(signalCtx)
	defer cancelChildren()

	started, err := startSession(childCtx, signalCtx, opts)
	if err != nil {
		return err
	}
	defer stopProcess(started.chromeProcess)
	defer stopProcess(started.mihomoProcess)
	if !opts.Keep {
		defer os.RemoveAll(started.session.RuntimeDir)
	}

	printOpts := opts
	printOpts.Group = started.session.Group
	printOpts.DefaultNode = started.session.Node
	if err := a.printRunEndpoints(printOpts, started.controllerURL, started.session.RuntimeDir); err != nil {
		return err
	}

	<-signalCtx.Done()
	return nil
}

// Start starts a persistent isolated browser session.
func (a *App) Start(ctx context.Context, opts Options) error {
	if err := requireNode(opts); err != nil {
		return err
	}

	if _, err := state.Load(opts.StateDir); err == nil {
		return fmt.Errorf("session already exists at %s; run `browsebox status` or `browsebox stop` before starting another session", state.Path(opts.StateDir))
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load session state: %w", err)
	}

	started, err := startSession(ctx, ctx, opts)
	if err != nil {
		return err
	}
	if err := state.Save(opts.StateDir, started.session); err != nil {
		stopProcess(started.chromeProcess)
		stopProcess(started.mihomoProcess)
		if !opts.Keep {
			_ = os.RemoveAll(started.session.RuntimeDir)
		}
		return fmt.Errorf("save session state: %w", err)
	}
	printOpts := opts
	printOpts.Group = started.session.Group
	printOpts.DefaultNode = started.session.Node
	return a.printRunEndpoints(printOpts, started.controllerURL, started.session.RuntimeDir)
}

// Status reports the current browsebox session status.
func (a *App) Status(ctx context.Context, opts Options) error {
	_ = ctx
	session, err := state.Load(opts.StateDir)
	if errors.Is(err, os.ErrNotExist) {
		_, writeErr := fmt.Fprintln(a.stdout, "No browsebox session is recorded.")
		return writeErr
	}
	if err != nil {
		return fmt.Errorf("load session state: %w", err)
	}
	return a.printSessionStatus(session)
}

// Stop stops a running browsebox session.
func (a *App) Stop(ctx context.Context, opts Options) error {
	_ = ctx
	session, err := state.Load(opts.StateDir)
	if errors.Is(err, os.ErrNotExist) {
		_, writeErr := fmt.Fprintln(a.stdout, "No browsebox session is recorded.")
		return writeErr
	}
	if err != nil {
		return fmt.Errorf("load session state: %w", err)
	}
	if session.ManagedBy != managedByBrowsebox {
		return fmt.Errorf("session state at %s is not managed by browsebox", state.Path(opts.StateDir))
	}
	if !opts.Keep && session.RuntimeDir != "" {
		if err := validateRuntimeDirForRemoval(session, opts.StateDir); err != nil {
			return err
		}
	}
	for _, target := range []managedProcessTarget{
		{name: "chrome", pid: session.ChromePID},
		{name: "mihomo", pid: session.MihomoPID},
	} {
		if target.pid <= 0 {
			continue
		}
		if err := stopManagedProcess(session, target); err != nil {
			return err
		}
	}
	if !opts.Keep && session.RuntimeDir != "" {
		if err := os.RemoveAll(session.RuntimeDir); err != nil {
			return fmt.Errorf("remove runtime dir: %w", err)
		}
	}
	if err := state.Remove(opts.StateDir); err != nil {
		return fmt.Errorf("remove session state: %w", err)
	}
	_, writeErr := fmt.Fprintln(a.stdout, "Browsebox session stopped")
	return writeErr
}

type startedSession struct {
	session       state.Session
	mihomoProcess process
	chromeProcess process
	controllerURL string
}

func startSession(processCtx, controlCtx context.Context, opts Options) (startedSession, error) {
	if opts.SelectFastest {
		if err := validateNodeTuning(opts); err != nil {
			return startedSession{}, err
		}
	} else if err := validateDelayTimeout(opts.DelayTimeoutMS); err != nil {
		return startedSession{}, err
	}
	if err := checkLocalPorts(opts.ProxyPort, opts.ControllerPort, opts.DevToolsPort); err != nil {
		return startedSession{}, fmt.Errorf("check local ports: %w", err)
	}
	if err := resolveStartupProxyGroup(controlCtx, &opts); err != nil {
		return startedSession{}, err
	}

	sourceConfig, err := readFile(opts.SourceConfigPath)
	if err != nil {
		return startedSession{}, fmt.Errorf("read source config: %w", err)
	}

	runtimeDir, err := createRuntimeDir(opts.RuntimeDir)
	if err != nil {
		return startedSession{}, fmt.Errorf("create runtime dir: %w", err)
	}
	cleanupRuntime := true
	defer func() {
		if cleanupRuntime && !opts.Keep {
			_ = os.RemoveAll(runtimeDir)
		}
	}()
	if err := prepareMihomoDataFiles(opts.SourceConfigPath, opts.RuntimeCacheDir, runtimeDir); err != nil {
		return startedSession{}, fmt.Errorf("prepare mihomo data files: %w", err)
	}

	rewritten := mihomo.RewriteConfig(string(sourceConfig), mihomo.RuntimeConfigOptions{
		ProxyPort:      opts.ProxyPort,
		ControllerPort: opts.ControllerPort,
		InterfaceName:  opts.MihomoInterfaceName,
		Group:          opts.Group,
		Node:           opts.DefaultNode,
	})
	configPath, err := writeRuntimeConfig(runtimeDir, []byte(rewritten))
	if err != nil {
		return startedSession{}, fmt.Errorf("write runtime config: %w", err)
	}

	mihomoProcess, err := startMihomoProcess(processCtx, opts.MihomoBinaryPath, runtimeDir, configPath)
	if err != nil {
		return startedSession{}, fmt.Errorf("start mihomo: %w", err)
	}
	cleanupMihomo := true
	defer func() {
		if cleanupMihomo {
			stopProcess(mihomoProcess)
		}
	}()

	controllerURL := fmt.Sprintf("http://127.0.0.1:%d", opts.ControllerPort)
	tempClient := mihomo.NewTCPClient(controllerURL)
	if err := waitControllerReady(controlCtx, tempClient, opts.Group); err != nil {
		return startedSession{}, fmt.Errorf("wait for temp controller: %w", err)
	}
	selectedNode, err := startupNode(controlCtx, tempClient, opts)
	if err != nil {
		return startedSession{}, err
	}
	selectCtx, cancelSelect := context.WithTimeout(controlCtx, selectNodeTimeout)
	selectErr := tempClient.SelectNode(selectCtx, opts.Group, selectedNode)
	cancelSelect()
	if selectErr != nil {
		return startedSession{}, fmt.Errorf("select temp node %q in group %q: %w", selectedNode, opts.Group, selectErr)
	}
	if err := checkHealthURLs(controlCtx, tempClient, selectedNode, opts.HealthURLs, opts.DelayTimeoutMS); err != nil {
		return startedSession{}, err
	}

	profileDir := chromeProfileDir(opts, runtimeDir)
	chromeProcess, err := startChrome(processCtx, opts.ChromeBinaryPath, browser.Options{
		UserDataDir:  profileDir,
		ProxyPort:    opts.ProxyPort,
		DevToolsPort: opts.DevToolsPort,
		Headless:     opts.BrowserHeadless,
		ChromeArgs:   opts.ChromeArgs,
		URL:          opts.TargetURL,
	})
	if err != nil {
		return startedSession{}, fmt.Errorf("start chrome: %w", err)
	}

	cleanupRuntime = false
	cleanupMihomo = false
	return startedSession{
		session: state.Session{
			ManagedBy:        managedByBrowsebox,
			MihomoPID:        mihomoProcess.PID(),
			ChromePID:        chromeProcess.PID(),
			ProxyPort:        opts.ProxyPort,
			ControllerPort:   opts.ControllerPort,
			DevToolsPort:     opts.DevToolsPort,
			RuntimeDir:       runtimeDir,
			ChromeDir:        profileDir,
			Group:            opts.Group,
			Node:             selectedNode,
			URL:              opts.TargetURL,
			StartedAt:        time.Now().UTC().Format(time.RFC3339),
			MihomoBinaryPath: opts.MihomoBinaryPath,
			ChromeBinaryPath: opts.ChromeBinaryPath,
		},
		mihomoProcess: mihomoProcess,
		chromeProcess: chromeProcess,
		controllerURL: controllerURL,
	}, nil
}

func startupNode(ctx context.Context, client *mihomo.Client, opts Options) (string, error) {
	if !opts.SelectFastest {
		return strings.TrimSpace(opts.DefaultNode), nil
	}

	group, err := lookupProxyGroup(ctx, client, opts.Group)
	if err != nil {
		return "", fmt.Errorf("lookup proxy group %q: %w", opts.Group, err)
	}
	targetURLs := nodeProbeURLs(opts)
	results := collectNodeDelays(ctx, client, group.All, targetURLs, opts.NodeProbeRounds, opts.NodeProbeIntervalMS, opts.NodesConcurrency, opts.DelayTimeoutMS)
	sortNodeDelayResults(results)
	for _, result := range results {
		if result.healthy {
			return result.name, nil
		}
	}
	return "", fmt.Errorf("select fastest node in group %q: no healthy nodes", opts.Group)
}

func requireNode(opts Options) error {
	if strings.TrimSpace(opts.DefaultNode) == "" && !opts.SelectFastest {
		return errors.New("--node is required for run and start unless --select-fastest is set")
	}
	return nil
}

func ensureLocalPortsAvailable(ports ...int) error {
	seen := make(map[int]bool, len(ports))
	for _, port := range ports {
		if port <= 0 {
			return fmt.Errorf("invalid port %d", port)
		}
		if seen[port] {
			continue
		}
		seen[port] = true
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return fmt.Errorf("127.0.0.1:%d is unavailable: %w", port, err)
		}
		if err := listener.Close(); err != nil {
			return fmt.Errorf("release 127.0.0.1:%d probe: %w", port, err)
		}
	}
	return nil
}

func createRuntimeDir(baseDir string) (string, error) {
	if baseDir == "" {
		return os.MkdirTemp("", fmt.Sprintf("browsebox-%d-", os.Getpid()))
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(baseDir, "browsebox-*")
}

var mihomoDataFileNames = []string{
	"Country.mmdb",
	"geoip.dat",
	"GeoIP.dat",
	"geosite.dat",
	"GeoSite.dat",
	"geoip.metadb",
}

func prepareMihomoDataFiles(sourceConfigPath, cacheDir, runtimeDir string) error {
	if strings.TrimSpace(cacheDir) == "" {
		return copyMihomoDataFiles(filepath.Dir(sourceConfigPath), runtimeDir)
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	if err := os.Chmod(cacheDir, 0o700); err != nil {
		return fmt.Errorf("chmod cache dir: %w", err)
	}
	if err := refreshMihomoDataCache(filepath.Dir(sourceConfigPath), cacheDir); err != nil {
		return err
	}
	return copyMihomoDataFiles(cacheDir, runtimeDir)
}

func refreshMihomoDataCache(sourceDir, cacheDir string) error {
	for _, name := range mihomoDataFileNames {
		sourcePath := filepath.Join(sourceDir, name)
		sourceInfo, err := os.Lstat(sourcePath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat source %s: %w", name, err)
		}
		if !sourceInfo.Mode().IsRegular() {
			continue
		}
		cachePath := filepath.Join(cacheDir, name)
		cacheInfo, err := os.Lstat(cachePath)
		if err == nil && !cacheInfo.Mode().IsRegular() {
			return fmt.Errorf("cache %s is not a regular file", name)
		}
		if err == nil && cacheInfo.Size() == sourceInfo.Size() && !sourceInfo.ModTime().After(cacheInfo.ModTime()) {
			continue
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat cache %s: %w", name, err)
		}
		if err := copyFile(sourcePath, cachePath); err != nil {
			return fmt.Errorf("cache %s: %w", name, err)
		}
	}
	return nil
}

func copyMihomoDataFiles(sourceDir, runtimeDir string) error {
	for _, name := range mihomoDataFileNames {
		if err := copyOptionalDataFile(filepath.Join(sourceDir, name), filepath.Join(runtimeDir, name)); err != nil {
			return fmt.Errorf("copy %s: %w", name, err)
		}
	}
	return nil
}

func copyOptionalDataFile(sourcePath, targetPath string) error {
	info, err := os.Lstat(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	return copyFile(sourcePath, targetPath)
}

func copyFile(sourcePath, targetPath string) error {
	if info, err := os.Lstat(sourcePath); err != nil {
		return fmt.Errorf("stat source: %w", err)
	} else if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file")
	}
	if info, err := os.Lstat(targetPath); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("target is not a regular file")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat target: %w", err)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}
	defer source.Close()

	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}
	_, copyErr := io.Copy(target, source)
	closeErr := target.Close()
	if copyErr != nil {
		return fmt.Errorf("write: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("write: %w", closeErr)
	}
	return nil
}

func chromeProfileDir(opts Options, runtimeDir string) string {
	if strings.TrimSpace(opts.ChromeProfileDir) != "" {
		return opts.ChromeProfileDir
	}
	return filepath.Join(runtimeDir, "chrome-profile")
}

func checkHealthURLs(ctx context.Context, client *mihomo.Client, node string, healthURLs []string, timeoutMS int) error {
	for _, healthURL := range healthURLs {
		trimmedURL := strings.TrimSpace(healthURL)
		if trimmedURL == "" {
			continue
		}
		healthCtx, cancelHealth := context.WithTimeout(ctx, nodeDelayContextTimeout(timeoutMS))
		delay, err := client.Delay(healthCtx, node, trimmedURL, timeoutMS)
		cancelHealth()
		if err != nil {
			return fmt.Errorf("health check %q through node %q: %w", trimmedURL, sanitizeDisplayName(node), err)
		}
		if delay.Error != "" {
			return fmt.Errorf("health check %q through node %q: %s", trimmedURL, sanitizeDisplayName(node), delay.Error)
		}
	}
	return nil
}

func waitControllerReady(ctx context.Context, client *mihomo.Client, group string) error {
	ctx, cancel := context.WithTimeout(ctx, controllerReadyTimeout)
	defer cancel()
	ticker := time.NewTicker(controllerReadyInterval)
	defer ticker.Stop()

	for {
		lookupCtx, cancelLookup := context.WithTimeout(ctx, controllerLookupTimeout)
		_, err := client.ProxyGroup(lookupCtx, group)
		cancelLookup()
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return err
		case <-ticker.C:
		}
	}
}

func (a *App) printRunEndpoints(opts Options, controllerURL, runtimeDir string) error {
	lines := []string{
		fmt.Sprintf("Proxy: http://127.0.0.1:%d", opts.ProxyPort),
		fmt.Sprintf("Controller: %s", controllerURL),
		fmt.Sprintf("DevTools: http://127.0.0.1:%d", opts.DevToolsPort),
		fmt.Sprintf("Selected: %s / %s", sanitizeDisplayName(opts.Group), sanitizeDisplayName(opts.DefaultNode)),
		fmt.Sprintf("Opened: %s", opts.TargetURL),
	}
	if opts.Keep {
		lines = append(lines, fmt.Sprintf("Cleanup: kept runtime dir %s", runtimeDir))
	} else {
		lines = append(lines, "Cleanup: runtime files will be removed on exit; use --keep to preserve them.")
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(a.stdout, line); err != nil {
			return err
		}
	}
	return nil
}

func processStatusText(pid int) string {
	if pid <= 0 {
		return "not recorded"
	}
	alive, err := processAlive(pid)
	if err != nil {
		return "unknown"
	}
	if alive {
		return "alive"
	}
	return "not running"
}

func (a *App) printSessionStatus(session state.Session) error {
	lines := []string{
		"Browsebox session recorded:",
		fmt.Sprintf("Mihomo PID: %d (%s)", session.MihomoPID, processStatusText(session.MihomoPID)),
		fmt.Sprintf("Chrome PID: %d (%s)", session.ChromePID, processStatusText(session.ChromePID)),
		fmt.Sprintf("Proxy: http://127.0.0.1:%d", session.ProxyPort),
		fmt.Sprintf("Controller: http://127.0.0.1:%d", session.ControllerPort),
		fmt.Sprintf("DevTools: http://127.0.0.1:%d", session.DevToolsPort),
		fmt.Sprintf("Selected: %s / %s", sanitizeDisplayName(session.Group), sanitizeDisplayName(session.Node)),
		fmt.Sprintf("Opened: %s", session.URL),
		fmt.Sprintf("Runtime: %s", session.RuntimeDir),
	}
	if session.ChromeDir != "" {
		lines = append(lines, fmt.Sprintf("Chrome profile: %s", session.ChromeDir))
	}
	if session.StartedAt != "" {
		lines = append(lines, fmt.Sprintf("Started: %s", session.StartedAt))
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(a.stdout, line); err != nil {
			return err
		}
	}
	return nil
}

type managedProcessTarget struct {
	name string
	pid  int
}

func stopManagedProcess(session state.Session, target managedProcessTarget) error {
	info, err := inspectProcess(target.pid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s process %d: %w", target.name, target.pid, err)
	}
	if err := validateProcessOwnership(info, target); err != nil {
		return err
	}
	if !processMatchesSession(info, session, target.name) {
		return fmt.Errorf("%s process %d does not match browsebox session", target.name, target.pid)
	}
	if err := signalProcess(target.pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("signal %s process %d: %w", target.name, target.pid, err)
	}
	deadline := time.Now().Add(processStopTimeout)
	for time.Now().Before(deadline) {
		alive, err := processAlive(target.pid)
		if err != nil {
			return fmt.Errorf("check %s process %d: %w", target.name, target.pid, err)
		}
		if !alive {
			return nil
		}
		time.Sleep(controllerReadyInterval)
	}
	info, err = inspectProcess(target.pid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reinspect %s process %d before kill: %w", target.name, target.pid, err)
	}
	if err := validateProcessOwnership(info, target); err != nil {
		return err
	}
	if !processMatchesSession(info, session, target.name) {
		return fmt.Errorf("%s process %d does not match browsebox session before kill", target.name, target.pid)
	}
	if err := signalProcess(target.pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill %s process %d: %w", target.name, target.pid, err)
	}
	return nil
}

func validateProcessOwnership(info processInfo, target managedProcessTarget) error {
	if info.Owner == "" {
		return fmt.Errorf("%s process %d owner cannot be verified", target.name, target.pid)
	}
	owner, err := currentProcessOwner()
	if err != nil || owner == "" {
		if err == nil {
			err = errors.New("empty current owner")
		}
		return fmt.Errorf("current process owner cannot be verified: %w", err)
	}
	if info.Owner != owner {
		return fmt.Errorf("%s process %d owner %q does not match current owner %q", target.name, target.pid, info.Owner, owner)
	}
	return nil
}

func processMatchesSession(info processInfo, session state.Session, name string) bool {
	command := info.Command
	switch name {
	case "chrome":
		return commandContainsPath(command, session.ChromeBinaryPath) && session.ChromeDir != "" && strings.Contains(command, session.ChromeDir)
	case "mihomo":
		return commandContainsPath(command, session.MihomoBinaryPath) && session.RuntimeDir != "" && strings.Contains(command, session.RuntimeDir)
	default:
		return false
	}
}

func commandContainsPath(command, path string) bool {
	if path == "" {
		return true
	}
	if strings.Contains(command, path) {
		return true
	}
	base := filepath.Base(path)
	return base != "." && base != string(filepath.Separator) && strings.Contains(command, base)
}

func validateRuntimeDirForRemoval(session state.Session, stateDir string) error {
	runtimeDir := filepath.Clean(session.RuntimeDir)
	if session.RuntimeDir != runtimeDir || !filepath.IsAbs(runtimeDir) {
		return fmt.Errorf("unsafe runtime dir %q: must be a clean absolute path", session.RuntimeDir)
	}
	matched, err := filepath.Match("browsebox-*", filepath.Base(runtimeDir))
	if err != nil || !matched {
		return fmt.Errorf("unsafe runtime dir %q: basename must match browsebox-*", session.RuntimeDir)
	}
	if runtimeDir == string(filepath.Separator) {
		return fmt.Errorf("unsafe runtime dir %q: refusing to remove root", session.RuntimeDir)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" && runtimeDir == filepath.Clean(home) {
		return fmt.Errorf("unsafe runtime dir %q: refusing to remove home dir", session.RuntimeDir)
	}
	if stateDir != "" && runtimeDir == filepath.Clean(stateDir) {
		return fmt.Errorf("unsafe runtime dir %q: refusing to remove state dir", session.RuntimeDir)
	}
	return nil
}

func stopProcess(p process) {
	if p == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		_ = p.Wait()
		close(done)
	}()
	_ = p.Signal(syscall.SIGTERM)
	select {
	case <-done:
	case <-time.After(processStopTimeout):
		_ = p.Kill()
		<-done
	}
}

func sanitizeDisplayName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case 0x1b:
			b.WriteString(`\x1b`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\x%02x`, r)
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
