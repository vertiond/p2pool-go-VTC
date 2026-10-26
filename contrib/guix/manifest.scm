;; Packages for the reproducible p2pool-go-VTC build. guix-build evaluates this
;; with HOST set to the platform being built (or "fetch" for the step that
;; downloads the Go modules).
;;
;; The node is pure Go and builds with CGO_ENABLED=0, as the CI build does, so
;; Go's own linker cross-compiles both platforms and no C toolchain is needed.
;; guix-build pins the Guix commit Bitcoin Core v32.0 pins, so a machine that
;; has built Bitcoin Core v32, Vertcoin Core's 32.x branch or the One Click
;; Miner already has most of these packages in its store.

(use-modules (gnu packages))

(packages->manifest
 (append
  (map specification->package
       (list "bash-minimal"
             "coreutils-minimal"
             "findutils"
             "grep"
             "sed"
             "tar"
             "zip"
             ;; go.mod says go 1.25.7; this Guix commit has 1.25.8
             "go@1.25"))
  (let ((target (getenv "HOST")))
    (cond ((string=? target "fetch")
           ;; The only step with network access, which needs CA certificates
           (list (specification->package "nss-certs")))
          ((member target '("x86_64-linux-gnu" "x86_64-w64-mingw32"))
           '())
          (else (error "unknown HOST" target))))))
