package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	catalogv1alpha1 "github.com/dcm-project/control-plane/api/catalog/v1alpha1"
	catalogservice "github.com/dcm-project/control-plane/internal/catalog/service"
	gitopsstore "github.com/dcm-project/control-plane/internal/gitops/store"
	gitopsmodel "github.com/dcm-project/control-plane/internal/gitops/store/model"
)

type stubGitClient struct {
	commit string
	dir    string
}

func (g *stubGitClient) CloneOrFetch(context.Context, string, string, string) (string, error) {
	return g.commit, nil
}
func (g *stubGitClient) WorkDir(string) string { return g.dir }

type stubGitRepositoryStore struct {
	states   []string
	messages []string
	commits  []string
}

func (s *stubGitRepositoryStore) List(context.Context, *gitopsstore.GitRepositoryListOptions) (*gitopsstore.GitRepositoryListResult, error) {
	return nil, nil
}
func (s *stubGitRepositoryStore) ListAll(context.Context) (gitopsmodel.GitRepositoryList, error) {
	return nil, nil
}
func (s *stubGitRepositoryStore) Get(context.Context, string) (*gitopsmodel.GitRepository, error) {
	return nil, nil
}
func (s *stubGitRepositoryStore) Create(context.Context, gitopsmodel.GitRepository) (*gitopsmodel.GitRepository, error) {
	return nil, nil
}
func (s *stubGitRepositoryStore) Update(context.Context, gitopsmodel.GitRepository) (*gitopsmodel.GitRepository, error) {
	return nil, nil
}
func (s *stubGitRepositoryStore) Delete(context.Context, string) error { return nil }
func (s *stubGitRepositoryStore) UpdateSyncStatus(_ context.Context, _, syncState, statusMessage, lastSyncedCommit string) error {
	s.states = append(s.states, syncState)
	s.messages = append(s.messages, statusMessage)
	s.commits = append(s.commits, lastSyncedCommit)
	return nil
}

type stubManagedInstanceStore struct{ ids []string }

func (s *stubManagedInstanceStore) ListByRepo(context.Context, string) ([]string, error) {
	return s.ids, nil
}
func (s *stubManagedInstanceStore) Add(context.Context, string, string) error    { return nil }
func (s *stubManagedInstanceStore) Remove(context.Context, string, string) error { return nil }

type stubGitopsStore struct {
	repos   *stubGitRepositoryStore
	managed *stubManagedInstanceStore
}

func (s *stubGitopsStore) Close() error                                 { return nil }
func (s *stubGitopsStore) GitRepository() gitopsstore.GitRepository     { return s.repos }
func (s *stubGitopsStore) ManagedInstance() gitopsstore.ManagedInstance { return s.managed }

func writeManifest(dir, filename, userValuePath string) {
	manifest := fmt.Sprintf(`apiVersion: v1alpha1
kind: CatalogItemInstance
metadata:
  name: decl-app
spec:
  catalog_item_id: two-tier
  user_values:
    - resource: backend
      path: %s
      value: decl-backend-1
`, userValuePath)
	Expect(os.WriteFile(filepath.Join(dir, filename), []byte(manifest), 0o600)).To(Succeed())
}

var _ = Describe("Reconcile desired-state validation", func() {
	var (
		dir     *string
		repos   *stubGitRepositoryStore
		st      *stubGitopsStore
		instSvc *stubCatalogItemInstanceService
		itemSvc *stubCatalogItemService
	)

	BeforeEach(func() {
		d := GinkgoT().TempDir()
		dir = &d
		repos = &stubGitRepositoryStore{}
		st = &stubGitopsStore{repos: repos, managed: &stubManagedInstanceStore{}}
		instSvc = &stubCatalogItemInstanceService{}
		itemSvc = &stubCatalogItemService{item: catalogItemWithFields(nil)}
	})

	reconcile := func() error {
		r := NewReconciler(st, instSvc, itemSvc, &stubGitClient{commit: "5195619", dir: *dir})
		return r.Reconcile(context.Background(), gitopsmodel.GitRepository{
			ID:               "apps-repo",
			URL:              "https://example.com/apps.git",
			Branch:           "main",
			Path:             ".",
			LastSyncedCommit: "old-valid-commit",
		})
	}

	It("reports ERROR when an already-managed instance has an unapplyable user value path", func() {
		// decl-app was created from a previous, valid commit.
		st.managed.ids = []string{"decl-app"}
		writeManifest(*dir, "instance.yaml", "does.not.exist")
		instSvc.validateFn = func(context.Context, catalogv1alpha1.CatalogItemInstanceSpec) error {
			return fmt.Errorf("%w: does.not.exist", catalogservice.ErrUserValuePathNotFound)
		}

		err := reconcile()

		Expect(err).To(MatchError(ContainSubstring("1 invalid desired instances at commit 5195619")))
		Expect(repos.states).To(Equal([]string{"ERROR"}))
		Expect(repos.messages[0]).To(ContainSubstring("user value path not found in catalog item fields: does.not.exist"))
		// last_synced_commit is not advanced, so the last valid state is preserved
		// and the bad commit is retried on the next cycle.
		Expect(repos.commits).To(Equal([]string{""}))
		// The previously valid managed instance is left untouched.
		Expect(instSvc.deletedIDs).To(BeEmpty())
	})

	It("reports ERROR for an invalid new instance without creating anything", func() {
		writeManifest(*dir, "instance.yaml", "does.not.exist")
		instSvc.validateFn = func(context.Context, catalogv1alpha1.CatalogItemInstanceSpec) error {
			return fmt.Errorf("%w: does.not.exist", catalogservice.ErrUserValuePathNotFound)
		}
		created := false
		instSvc.createFn = func(context.Context, *catalogservice.CreateCatalogItemInstanceRequest) (*catalogv1alpha1.CatalogItemInstance, error) {
			created = true
			return &catalogv1alpha1.CatalogItemInstance{}, nil
		}

		Expect(reconcile()).To(HaveOccurred())
		Expect(repos.states).To(Equal([]string{"ERROR"}))
		Expect(created).To(BeFalse())
	})

	It("validates every desired instance, including ones with no lifecycle change", func() {
		st.managed.ids = []string{"decl-app"}
		writeManifest(*dir, "instance.yaml", "metadata.name")

		Expect(reconcile()).To(Succeed())
		Expect(instSvc.validatedIDs).To(Equal([]string{"two-tier"}))
		Expect(repos.states).To(Equal([]string{"SYNCED"}))
		Expect(repos.commits).To(Equal([]string{"5195619"}))
	})
})
