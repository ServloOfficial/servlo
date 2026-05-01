package cli

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveAutoloadRoots_ReadsComposerPSR4(t *testing.T) {
	site := t.TempDir()
	writeTestFile(t, filepath.Join(site, "composer.json"), `{
		"autoload": {
			"psr-4": {
				"App\\": "src/",
				"App\\Tests\\": ["tests"]
			}
		}
	}`)
	if err := os.MkdirAll(filepath.Join(site, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(site, "tests"), 0755); err != nil {
		t.Fatal(err)
	}

	got := resolveAutoloadRoots(site)
	if !slices.Contains(got, filepath.Join(site, "src")) {
		t.Errorf("expected src/ in roots, got %v", got)
	}
	if !slices.Contains(got, filepath.Join(site, "tests")) {
		t.Errorf("expected tests/ in roots, got %v", got)
	}
}

func TestResolveAutoloadRoots_LaravelDefaults(t *testing.T) {
	site := t.TempDir()
	if err := os.MkdirAll(filepath.Join(site, "app", "Models"), 0755); err != nil {
		t.Fatal(err)
	}
	got := resolveAutoloadRoots(site)
	if !slices.Contains(got, filepath.Join(site, "app", "Models")) {
		t.Errorf("expected app/Models in roots, got %v", got)
	}
}

func TestCollectTinkerSymbols_FindsSymfonyEntities(t *testing.T) {
	site := t.TempDir()
	writeTestFile(t, filepath.Join(site, "composer.json"), `{
		"autoload": { "psr-4": { "App\\": "src/" } }
	}`)
	writeTestFile(t, filepath.Join(site, "src", "Entity", "Product.php"), `<?php
namespace App\Entity;
use Doctrine\ORM\Mapping as ORM;

#[ORM\Entity]
class Product {
	private int $id;
}
`)
	writeTestFile(t, filepath.Join(site, "src", "Service", "PriceCalculator.php"), `<?php
namespace App\Service;
class PriceCalculator {}
`)

	syms := CollectTinkerSymbols(site)
	if !slices.Contains(syms.Models, "Product") {
		t.Errorf("expected Product flagged as model, got models=%v", syms.Models)
	}
	if !slices.Contains(syms.Classes, "PriceCalculator") {
		t.Errorf("expected PriceCalculator in classes, got %v", syms.Classes)
	}
	if slices.Contains(syms.Models, "PriceCalculator") {
		t.Errorf("PriceCalculator should not be flagged as model")
	}
}

func TestCollectTinkerSymbols_FindsLaravelModels(t *testing.T) {
	site := t.TempDir()
	writeTestFile(t, filepath.Join(site, "composer.json"), `{
		"autoload": { "psr-4": { "App\\": "app/" } }
	}`)
	writeTestFile(t, filepath.Join(site, "app", "Models", "User.php"), `<?php
namespace App\Models;
use Illuminate\Database\Eloquent\Model;
class User extends Model {}
`)
	writeTestFile(t, filepath.Join(site, "app", "Http", "Controllers", "UserController.php"), `<?php
namespace App\Http\Controllers;
class UserController {}
`)

	syms := CollectTinkerSymbols(site)
	if !slices.Contains(syms.Models, "User") {
		t.Errorf("expected User in models, got %v", syms.Models)
	}
	if !slices.Contains(syms.Classes, "UserController") {
		t.Errorf("expected UserController in classes, got %v", syms.Classes)
	}
}
