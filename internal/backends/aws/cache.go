package aws

// GetSecretCache returns the secrets cache for a target.
func (acp *ClientPool) GetSecretCache(targetName string) map[string]string {
	acp.mu.RLock()
	defer acp.mu.RUnlock()

	if cache, exists := acp.secretsCache[targetName]; exists {
		return cache
	}

	acp.mu.RUnlock()
	acp.mu.Lock()
	acp.secretsCache[targetName] = make(map[string]string)
	cache := acp.secretsCache[targetName]
	acp.mu.Unlock()
	acp.mu.RLock()

	return cache
}

// GetParamCache returns the parameters cache for a target.
func (acp *ClientPool) GetParamCache(targetName string) map[string]string {
	acp.mu.RLock()
	defer acp.mu.RUnlock()

	if cache, exists := acp.paramsCache[targetName]; exists {
		return cache
	}

	acp.mu.RUnlock()
	acp.mu.Lock()
	acp.paramsCache[targetName] = make(map[string]string)
	cache := acp.paramsCache[targetName]
	acp.mu.Unlock()
	acp.mu.RLock()

	return cache
}

// SetSecretCache sets a secret value in the cache for a target.
func (acp *ClientPool) SetSecretCache(targetName, secret, value string) {
	acp.mu.Lock()
	defer acp.mu.Unlock()

	if _, exists := acp.secretsCache[targetName]; !exists {
		acp.secretsCache[targetName] = make(map[string]string)
	}
	acp.secretsCache[targetName][secret] = value
}

// SetParamCache sets a parameter value in the cache for a target.
func (acp *ClientPool) SetParamCache(targetName, param, value string) {
	acp.mu.Lock()
	defer acp.mu.Unlock()

	if _, exists := acp.paramsCache[targetName]; !exists {
		acp.paramsCache[targetName] = make(map[string]string)
	}
	acp.paramsCache[targetName][param] = value
}
