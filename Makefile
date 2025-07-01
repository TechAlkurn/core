up:
	@if [ -z "$(m)" ]; then \
		echo "Please provide a commit message using: make up m=\"<your message>\""; \
		exit 1; \
	fi
	git add .
	git commit -m "$(m)"
	git push origin main