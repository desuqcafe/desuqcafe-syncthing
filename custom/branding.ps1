# Single source of truth for desuqcafe Syncthing branding.
#
# Dot-source this from build/packaging scripts:  . "$PSScriptRoot\branding.ps1"
#
# Everything user-visible about the fork is defined here so that renaming the
# product is a one-file change and never touches upstream Syncthing source.

$Brand = @{
    # Display name, shown in the installer, Start Menu and Add/Remove Programs.
    Product     = 'desuqcafe Syncthing'

    # Base name of the executable (no extension) and of the install folder.
    Binary      = 'desuq-syncthing'

    # Publisher shown in file properties and Add/Remove Programs.
    Company     = 'desuqcafe'

    Description = 'desuqcafe Syncthing - Continuous File Synchronization'

    # Config + database directory under %LOCALAPPDATA%. Deliberately distinct
    # from upstream's "Syncthing" so this fork can coexist with a stock install.
    DataDirName = 'desuqcafe-syncthing'

    # Stable installer identity (bare GUID; the installer script adds the braces).
    # NEVER change this after the first release --
    # Inno Setup uses it to recognise and upgrade existing installations.
    AppId       = '33BDCF88-0798-4D57-906E-CE272E9E3AD3'

    Url         = 'https://github.com/desuqcafe/desuqcafe-syncthing'
}
