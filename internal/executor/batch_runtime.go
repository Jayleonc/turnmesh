package executor

import (
	"context"
	"errors"
	"sync"

	"github.com/Jayleonc/turnmesh/internal/core"
)

// BatchMode describes how a batch is executed.
type BatchMode string

const (
	BatchModeConcurrent BatchMode = "concurrent"
	BatchModeSerial     BatchMode = "serial"
)

// ToolBatch groups tool calls that share the same execution mode.
type ToolBatch struct {
	Mode    BatchMode
	Indexes []int
	Calls   []core.ToolInvocation
}

// BatchItem captures one tool result within a batch run.
type BatchItem struct {
	Index      int
	Invocation core.ToolInvocation
	Result     core.ToolResult
	Executed   bool
	Discarded  bool
}

// BatchReport is the ordered outcome of a batch run.
type BatchReport struct {
	Plan         []ToolBatch
	Items        []BatchItem
	Results      []core.ToolResult
	Failed       bool
	Error        error
	FallbackUsed bool
}

// BatchEventKind describes streaming progress updates.
type BatchEventKind string

const (
	BatchEventBatchStarted   BatchEventKind = "batch_started"
	BatchEventItemCompleted   BatchEventKind = "item_completed"
	BatchEventItemDiscarded   BatchEventKind = "item_discarded"
	BatchEventBatchCompleted  BatchEventKind = "batch_completed"
	BatchEventCompleted      BatchEventKind = "completed"
)

// BatchEvent is emitted while a batch run is executing.
type BatchEvent struct {
	Kind       BatchEventKind
	BatchIndex int
	ItemIndex  int
	Batch      ToolBatch
	Item       *BatchItem
	Error      error
}

// BatchStream exposes live events and the final report.
type BatchStream struct {
	Events <-chan BatchEvent
	Done   <-chan BatchReport
}

// BatchRuntime executes tool calls as serial or concurrent batches.
type BatchRuntime interface {
	Plan(ctx context.Context, calls []core.ToolInvocation) ([]ToolBatch, error)
	Run(ctx context.Context, calls []core.ToolInvocation) (BatchReport, error)
	Stream(ctx context.Context, calls []core.ToolInvocation) (BatchStream, error)
}

// BatchRuntimeOption customizes the runtime behavior.
type BatchRuntimeOption func(*BatchRuntimeEngine)

// WithSerialFallback forces every call to run as its own serial batch.
func WithSerialFallback(enabled bool) BatchRuntimeOption {
	return func(runtime *BatchRuntimeEngine) {
		runtime.serialFallback = enabled
	}
}

// BatchRuntimeEngine is the default batch/streaming runtime.
type BatchRuntimeEngine struct {
	runtime        Runtime
	serialFallback bool
}

// NewBatchRuntime creates a runtime backed by the provided tool registry/runtime.
func NewBatchRuntime(runtime Runtime, opts ...BatchRuntimeOption) *BatchRuntimeEngine {
	if runtime == nil {
		runtime = NewRegistryStore()
	}

	engine := &BatchRuntimeEngine{runtime: runtime}
	for _, opt := range opts {
		if opt != nil {
			opt(engine)
		}
	}

	return engine
}

// Plan partitions tool calls into concurrent and serial batches.
func (r *BatchRuntimeEngine) Plan(ctx context.Context, calls []core.ToolInvocation) ([]ToolBatch, error) {
	if ctx == nil {
		return nil, errors.New("executor: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(calls) == 0 {
		return nil, nil
	}

	if r != nil && r.serialFallback {
		return planSerial(calls), nil
	}

	return planByConcurrencySafe(r, calls), nil
}

// Run executes the batch plan and returns the ordered report.
func (r *BatchRuntimeEngine) Run(ctx context.Context, calls []core.ToolInvocation) (BatchReport, error) {
	if ctx == nil {
		return BatchReport{}, errors.New("executor: nil context")
	}
	if err := ctx.Err(); err != nil {
		return BatchReport{}, err
	}

	plan, err := r.Plan(ctx, calls)
	if err != nil {
		return BatchReport{}, err
	}

	report := r.execute(ctx, plan, calls, nil)
	return report, report.Error
}

// Stream executes the batch plan and streams progress updates.
func (r *BatchRuntimeEngine) Stream(ctx context.Context, calls []core.ToolInvocation) (BatchStream, error) {
	if ctx == nil {
		return BatchStream{}, errors.New("executor: nil context")
	}
	if err := ctx.Err(); err != nil {
		return BatchStream{}, err
	}

	plan, err := r.Plan(ctx, calls)
	if err != nil {
		return BatchStream{}, err
	}

	events := make(chan BatchEvent, len(calls)+len(plan)+1)
	done := make(chan BatchReport, 1)

	go func() {
		defer close(events)
		defer close(done)

		report := r.execute(ctx, plan, calls, func(event BatchEvent) {
			events <- event
		})
		done <- report
	}()

	return BatchStream{Events: events, Done: done}, nil
}

func (r *BatchRuntimeEngine) execute(
	ctx context.Context,
	plan []ToolBatch,
	calls []core.ToolInvocation,
	emit func(BatchEvent),
) BatchReport {
	report := BatchReport{
		Plan: clonePlan(plan),
	}
	if len(calls) == 0 {
		return report
	}

	items := make([]BatchItem, len(calls))
	// Failure cascade strategy:
	// the first failed item cancels the remainder of the current batch and
	// marks all subsequent batches as discarded. This keeps the runtime
	// deterministic while still allowing the concurrent batch to finish
	// collecting in-flight results.
	for batchIndex, batch := range plan {
		if err := ctx.Err(); err != nil {
			report.Failed = true
			report.Error = err
			r.discardRemaining(items, calls, plan, batchIndex, 0, err, emit)
			break
		}

		if emit != nil {
			emit(BatchEvent{
				Kind:       BatchEventBatchStarted,
				BatchIndex: batchIndex,
				Batch:      cloneBatch(batch),
			})
		}

		var failure error
		switch batch.Mode {
		case BatchModeSerial:
			failure = r.runSerialBatch(ctx, batchIndex, batch, items, calls, emit)
		default:
			failure = r.runConcurrentBatch(ctx, batchIndex, batch, items, calls, emit)
		}

		if failure != nil {
			report.Failed = true
			report.Error = failure
			r.discardRemaining(items, calls, plan, batchIndex+1, 0, failure, emit)
			break
		}

		if emit != nil {
			emit(BatchEvent{
				Kind:       BatchEventBatchCompleted,
				BatchIndex: batchIndex,
				Batch:      cloneBatch(batch),
			})
		}
	}

	report.Items = cloneItems(items)
	report.Results = orderedResults(items)
	report.FallbackUsed = r != nil && r.serialFallback
	if report.Failed && report.Error == nil {
		report.Error = core.NewError(core.ErrorCodeInternal, "tool batch failed")
	}
	if emit != nil {
		emit(BatchEvent{
			Kind:  BatchEventCompleted,
			Error: report.Error,
		})
	}

	return report
}

func (r *BatchRuntimeEngine) runSerialBatch(
	ctx context.Context,
	batchIndex int,
	batch ToolBatch,
	items []BatchItem,
	calls []core.ToolInvocation,
	emit func(BatchEvent),
) error {
	for localIndex, call := range batch.Calls {
		if err := ctx.Err(); err != nil {
			r.discardIndexes(items, calls, batchIndex, batch.Indexes[localIndex:], err, emit)
			return err
		}

		globalIndex := batch.Indexes[localIndex]
		item := r.executeCall(ctx, globalIndex, call)
		items[globalIndex] = item
		if emit != nil {
			cloned := cloneBatchItem(item)
			emit(BatchEvent{
				Kind:       BatchEventItemCompleted,
				BatchIndex: batchIndex,
				ItemIndex:  globalIndex,
				Batch:      cloneBatch(batch),
				Item:       &cloned,
				Error:      item.Result.Error,
			})
		}

		if itemFailed(item) {
			r.discardIndexes(items, calls, batchIndex, batch.Indexes[localIndex+1:], failureError(item), emit)
			return failureError(item)
		}
	}

	return nil
}

func (r *BatchRuntimeEngine) runConcurrentBatch(
	ctx context.Context,
	batchIndex int,
	batch ToolBatch,
	items []BatchItem,
	calls []core.ToolInvocation,
	emit func(BatchEvent),
) error {
	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type completion struct {
		index int
		item  BatchItem
	}

	results := make(chan completion, len(batch.Calls))
	var wg sync.WaitGroup
	for localIndex, call := range batch.Calls {
		wg.Add(1)
		go func(localIndex int, call core.ToolInvocation) {
			defer wg.Done()
			globalIndex := batch.Indexes[localIndex]
			results <- completion{
				index: globalIndex,
				item:  r.executeCall(batchCtx, globalIndex, call),
			}
		}(localIndex, cloneToolInvocation(call))
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var failure error
	for completion := range results {
		items[completion.index] = completion.item
		if emit != nil {
			cloned := cloneBatchItem(completion.item)
			emit(BatchEvent{
				Kind:       BatchEventItemCompleted,
				BatchIndex: batchIndex,
				ItemIndex:  completion.index,
				Batch:      cloneBatch(batch),
				Item:       &cloned,
				Error:      completion.item.Result.Error,
			})
		}

		if failure == nil && itemFailed(completion.item) {
			failure = failureError(completion.item)
			cancel()
		}
	}

	return failure
}

func (r *BatchRuntimeEngine) executeCall(ctx context.Context, index int, call core.ToolInvocation) BatchItem {
	item := BatchItem{
		Index:      index,
		Invocation: cloneToolInvocation(call),
		Executed:   true,
	}

	if r == nil || r.runtime == nil {
		item.Result = core.ToolResult{
			InvocationID: call.ID,
			Tool:         call.Tool,
			Status:       core.ToolStatusFailed,
			Error:        core.NewError(core.ErrorCodeUnsupported, "tool runtime not configured"),
		}
		return item
	}

	outcome, execErr := r.runtime.Execute(ctx, call.Tool, toolRequestFromInvocation(call))
	item.Result = toolResultFromOutcome(call, outcome, execErr)
	return item
}

func (r *BatchRuntimeEngine) discardRemaining(
	items []BatchItem,
	calls []core.ToolInvocation,
	plan []ToolBatch,
	startBatch int,
	startLocalIndex int,
	reason error,
	emit func(BatchEvent),
) {
	if startBatch >= len(plan) {
		return
	}

	for batchIndex := startBatch; batchIndex < len(plan); batchIndex++ {
		batch := plan[batchIndex]
		localStart := 0
		if batchIndex == startBatch {
			localStart = startLocalIndex
		}
		r.discardIndexes(items, calls, batchIndex, batch.Indexes[localStart:], reason, emit)
	}
}

func (r *BatchRuntimeEngine) discardIndexes(
	items []BatchItem,
	calls []core.ToolInvocation,
	batchIndex int,
	indexes []int,
	reason error,
	emit func(BatchEvent),
) {
	for _, index := range indexes {
		if index < 0 || index >= len(calls) {
			continue
		}
		if items[index].Executed || items[index].Discarded {
			continue
		}

		item := BatchItem{
			Index:      index,
			Invocation: cloneToolInvocation(calls[index]),
			Result:     discardedToolResult(calls[index], reason),
			Discarded:  true,
		}
		items[index] = item
		if emit != nil {
			cloned := cloneBatchItem(item)
			emit(BatchEvent{
				Kind:       BatchEventItemDiscarded,
				BatchIndex: batchIndex,
				ItemIndex:  index,
				Item:       &cloned,
				Error:      reason,
			})
		}
	}
}

func planByConcurrencySafe(r *BatchRuntimeEngine, calls []core.ToolInvocation) []ToolBatch {
	batches := make([]ToolBatch, 0, len(calls))
	current := ToolBatch{Mode: BatchModeConcurrent}

	flushCurrent := func() {
		if len(current.Calls) == 0 {
			return
		}
		batches = append(batches, current)
		current = ToolBatch{Mode: BatchModeConcurrent}
	}

	for index, call := range calls {
		if !r.isConcurrencySafe(call.Tool) {
			flushCurrent()
			batches = append(batches, ToolBatch{
				Mode:    BatchModeSerial,
				Indexes: []int{index},
				Calls:   []core.ToolInvocation{cloneToolInvocation(call)},
			})
			continue
		}

		current.Indexes = append(current.Indexes, index)
		current.Calls = append(current.Calls, cloneToolInvocation(call))
	}

	flushCurrent()
	return batches
}

func planSerial(calls []core.ToolInvocation) []ToolBatch {
	batches := make([]ToolBatch, 0, len(calls))
	for index, call := range calls {
		batches = append(batches, ToolBatch{
			Mode:    BatchModeSerial,
			Indexes: []int{index},
			Calls:   []core.ToolInvocation{cloneToolInvocation(call)},
		})
	}
	return batches
}

func (r *BatchRuntimeEngine) isConcurrencySafe(name string) bool {
	if r == nil || r.runtime == nil {
		return false
	}

	tool, ok := r.runtime.Lookup(name)
	if !ok || tool == nil {
		return false
	}

	return tool.Spec().ConcurrencySafe
}

func orderedResults(items []BatchItem) []core.ToolResult {
	results := make([]core.ToolResult, 0, len(items))
	for _, item := range items {
		results = append(results, cloneToolResult(item.Result))
	}
	return results
}

func cloneItems(items []BatchItem) []BatchItem {
	if len(items) == 0 {
		return nil
	}

	cloned := make([]BatchItem, len(items))
	for index, item := range items {
		cloned[index] = cloneBatchItem(item)
	}
	return cloned
}

func cloneBatchItem(item BatchItem) BatchItem {
	cloned := item
	cloned.Invocation = cloneToolInvocation(item.Invocation)
	cloned.Result = cloneToolResult(item.Result)
	return cloned
}

func clonePlan(plan []ToolBatch) []ToolBatch {
	if len(plan) == 0 {
		return nil
	}

	cloned := make([]ToolBatch, 0, len(plan))
	for _, batch := range plan {
		cloned = append(cloned, cloneBatch(batch))
	}
	return cloned
}

func cloneBatch(batch ToolBatch) ToolBatch {
	cloned := batch
	if len(batch.Indexes) > 0 {
		cloned.Indexes = append([]int(nil), batch.Indexes...)
	}
	if len(batch.Calls) > 0 {
		cloned.Calls = make([]core.ToolInvocation, 0, len(batch.Calls))
		for _, call := range batch.Calls {
			cloned.Calls = append(cloned.Calls, cloneToolInvocation(call))
		}
	}
	return cloned
}

func itemFailed(item BatchItem) bool {
	if item.Discarded {
		return true
	}
	return item.Result.Status != core.ToolStatusSucceeded
}

func failureError(item BatchItem) error {
	if item.Result.Error != nil {
		return item.Result.Error
	}
	if item.Discarded {
		return core.NewError(core.ErrorCodeCancelled, "tool discarded due to batch cascade")
	}
	return core.NewError(core.ErrorCodeInternal, "tool execution failed")
}

func discardedToolResult(call core.ToolInvocation, reason error) core.ToolResult {
	if reason == nil {
		reason = errors.New("tool discarded")
	}
	return core.ToolResult{
		InvocationID: call.ID,
		Tool:         call.Tool,
		Status:       core.ToolStatusCancelled,
		Error:        core.WrapError(core.ErrorCodeCancelled, "tool discarded due to batch cascade", reason),
		Metadata: map[string]string{
			"discarded": "true",
		},
	}
}

func toolResultFromOutcome(call core.ToolInvocation, outcome ToolOutcome, execErr error) core.ToolResult {
	result := core.ToolResult{
		InvocationID: call.ID,
		Tool:         call.Tool,
		Status:       outcome.Status,
		Output:       outcome.Output,
		Structured:   cloneRawMessage(outcome.Structured),
		Error:        outcome.Error,
		Duration:     outcome.Duration,
		Metadata:     cloneStringMap(outcome.Metadata),
	}
	if result.Status == "" {
		if execErr != nil {
			result.Status = core.ToolStatusFailed
		} else {
			result.Status = core.ToolStatusSucceeded
		}
	}
	if execErr != nil && result.Error == nil {
		result.Error = mapExecutorError(execErr)
	}
	if result.Status == "" {
		result.Status = core.ToolStatusSucceeded
	}
	return result
}

func cloneToolInvocation(call core.ToolInvocation) core.ToolInvocation {
	cloned := call
	cloned.Input = cloneRawMessage(call.Input)
	cloned.Arguments = cloneRawMessage(call.Arguments)
	cloned.Metadata = cloneStringMap(call.Metadata)
	return cloned
}

func cloneToolResult(result core.ToolResult) core.ToolResult {
	cloned := result
	cloned.Structured = cloneRawMessage(result.Structured)
	cloned.Metadata = cloneStringMap(result.Metadata)
	if result.Error != nil {
		cloned.Error = cloneCoreError(result.Error)
	}
	return cloned
}

func cloneCoreError(err *core.Error) *core.Error {
	if err == nil {
		return nil
	}

	cloned := *err
	if len(err.Details) > 0 {
		cloned.Details = make(map[string]string, len(err.Details))
		for key, value := range err.Details {
			cloned.Details[key] = value
		}
	}
	return &cloned
}
