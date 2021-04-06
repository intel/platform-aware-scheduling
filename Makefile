TOPTARGETS := all clean image build test

SUBDIRS := $(wildcard *-aware-scheduling/.)

$(TOPTARGETS): $(SUBDIRS)
$(SUBDIRS):
	$(MAKE) -C $@ $(MAKECMDGOALS)

.PHONY: $(TOPTARGETS) $(SUBDIRS)
