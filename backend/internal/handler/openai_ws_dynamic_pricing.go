package handler

import "github.com/Wei-Shaw/sub2api/internal/service"

func (k *openAIWSTurnBillingAPIKeys) quoteForTurn(turn int) *service.DynamicGroupPricing {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.quotes[turn]
}

func (k *openAIWSTurnBillingAPIKeys) setQuote(turn int, quote *service.DynamicGroupPricing) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.quotes == nil {
		k.quotes = make(map[int]*service.DynamicGroupPricing, 2)
	}
	for t := range k.quotes {
		if t < turn-1 {
			delete(k.quotes, t)
		}
	}
	k.quotes[turn] = quote
}

func (k *openAIWSTurnBillingAPIKeys) attachQuote(turn int, result *service.OpenAIForwardResult) {
	if result == nil {
		return
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	// An explicit nil also matters: this turn may have disabled dynamic pricing,
	// while a HTTP bridge still carries the original connection's context.
	if quote, ok := k.quotes[turn]; ok {
		result.DynamicGroupPricing = quote
	}
}
