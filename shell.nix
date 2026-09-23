{ pkgs ? import <nixpkgs> { } }:

pkgs.mkShell {
  packages = [
    pkgs.go
    pkgs.ffmpeg
  ];

  shellHook = ''
    echo "arabic-tts: go $(go version | cut -d' ' -f3), $(ffmpeg -version | head -1 | cut -d' ' -f1-3)"
  '';
}
