// What Swift sees of ios/bridge, linked in as libglane.a by build-go.sh.
// Both return a string the caller frees.
#include <stdlib.h>

char *GlaneOpen(const char *path);
char *GlaneRequest(const char *request);
