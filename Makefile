up:
	@if [ -z "$(m)" ]; then \
		m="update"; \
	else \
		m="$(m)"; \
	fi; \
	git add .; \
	git commit -m "$$m"; \
	git push origin main