MODULES := api cli core db

.PHONY: build test clean $(MODULES)

build:
	@for m in $(MODULES); do \
		echo "==> Building $$m"; \
		$(MAKE) -C $$m build; \
	done

test:
	@for m in $(MODULES); do \
		echo "==> Testing $$m"; \
		$(MAKE) -C $$m test; \
	done

clean:
	@for m in $(MODULES); do \
		echo "==> Cleaning $$m"; \
		$(MAKE) -C $$m clean; \
	done
