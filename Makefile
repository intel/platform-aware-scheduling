TOPTARGETS := all clean image build test e2e

SUBDIRS := $(wildcard *-aware-scheduling/.)

$(TOPTARGETS): $(SUBDIRS)
$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)

.PHONY: $(TOPTARGETS) $(SUBDIRS)
