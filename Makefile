TOPTARGETS := all clean image build test e2e

SUBDIRS := $(wildcard *-aware-scheduling/.) configurator

$(TOPTARGETS): $(SUBDIRS)
$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)

.PHONY: $(TOPTARGETS) $(SUBDIRS)
