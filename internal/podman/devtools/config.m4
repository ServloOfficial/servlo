PHP_ARG_ENABLE([servlo_devtools],
  [whether to enable servlo_devtools support],
  [AS_HELP_STRING([--enable-servlo-devtools], [Enable servlo_devtools])],
  [yes])

if test "$PHP_SERVLO_DEVTOOLS" != "no"; then
  AC_DEFINE(HAVE_SERVLO_DEVTOOLS, 1, [ Have servlo_devtools support ])
  PHP_NEW_EXTENSION(servlo_devtools, servlo_devtools.c, $ext_shared)
fi
