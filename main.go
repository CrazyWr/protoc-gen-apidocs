package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/Masterminds/sprig"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func main() {
	var flags flag.FlagSet
	format := flags.String("format", "markdown", "Format to use")
	templates := flags.String("templates", "", "Custom templates directory to use")

	opts := &protogen.Options{
		ParamFunc: flags.Set,
	}
	opts.Run(func(gen *protogen.Plugin) error {
		genOpts := GenOpts{
			Format:      *format,
			TemplateDir: *templates,
		}
		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}
			if err := genOpts.generateFile(gen, f); err != nil {
				return err
			}
		}
		return nil
	})
}

// GenOpts hold options for generation.
type GenOpts struct {
	Format      string
	TemplateDir string
}

// generateFile generates a _ascii.pb.go file containing gRPC service definitions.
func (o *GenOpts) generateFile(gen *protogen.Plugin, file *protogen.File) error {
	filename := file.GeneratedFilenamePrefix + "." + o.Format
	g := gen.NewGeneratedFile(filename, file.GoImportPath)
	if err := o.renderTemplate(file, g); err != nil {
		return fmt.Errorf("issue generating %v: %w", filename, err)
	}
	return nil
}

func longName(d protoreflect.Descriptor) string {
	p := d.Parent()
	if p != nil && p.Parent() != nil {
		return fmt.Sprintf("%v.%v", p.Name(), d.Name())
	}
	return fmt.Sprint(d.Name())
}

func (o *GenOpts) templateFuncMap() template.FuncMap {
	return map[string]interface{}{
		"anchor": func(str interface{}) string {
			return specialCharsPattern.ReplaceAllString(strings.ReplaceAll(fmt.Sprint(str), "/", "_"), "-")
		},
		"long_name": longName,
		"field_type": func(f *protogen.Field) string {
			if f.Message != nil {
				return longName(f.Message.Desc)
			}
			if f.Enum != nil {
				return longName(f.Enum.Desc)
			}
			return fmt.Sprint(f.Desc.Kind())
		},
		"full_field_type": func(f *protogen.Field) string {
			if f.Message != nil {
				return fmt.Sprint(f.Message.Desc.FullName())
			}
			if f.Enum != nil {
				return fmt.Sprint(f.Enum.Desc.FullName())
			}
			return fmt.Sprint(f.Desc.Kind())
		},
		"is_primitive": func(f *protogen.Field) bool {
			// TODO: consider oneof, enum, ...
			k := f.Desc.Kind()
			nonPrim := k == protoreflect.EnumKind || k == protoreflect.MessageKind || k == protoreflect.GroupKind
			return !nonPrim
		},
		"message_type": func(f *protogen.Message) string {
			if f == nil {
				return "(none)"
			}
			return fmt.Sprint(f.Desc.Name())
		},
		"full_message_type": func(f *protogen.Message) string {
			return fmt.Sprint(f.Desc.FullName())
		},
		"description": func(s interface{}) string {
			val := strings.TrimLeft(fmt.Sprint(s), "*/\n ")
			if strings.HasPrefix(val, "@exclude") {
				return ""
			}
			return val
		},
		"p":    pFilter,
		"para": paraFilter,
		"nobr": nobrFilter,
	}
}

//go:embed templates/*
var defaultTemplates embed.FS

func (o *GenOpts) getTemplateFS() (fs.FS, error) {
	if o.TemplateDir == "" {
		return fs.Sub(defaultTemplates, "templates")
	}
	tFS := os.DirFS(o.TemplateDir)
	return fs.Sub(tFS, o.TemplateDir)
}
func (o *GenOpts) renderTemplate(file *protogen.File, g *protogen.GeneratedFile) error {
	tFS, err := o.getTemplateFS()
	if err != nil {
		return err
	}
	t := template.New("file.tpl").Funcs(o.templateFuncMap()).Funcs(sprig.HtmlFuncMap())
	t, err = t.ParseFS(tFS, fmt.Sprintf("%v.tpl", o.Format))
	if err != nil {
		return err
	}
	return t.ExecuteTemplate(g, "output", file)
}

// Template Helpers

var (
	paraPattern         = regexp.MustCompile(`(\n|\r|\r\n)\s*`)
	spacePattern        = regexp.MustCompile("( )+")
	multiNewlinePattern = regexp.MustCompile(`(\r\n|\r|\n){2,}`)
	specialCharsPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)
)

func pFilter(content string) template.HTML {
	paragraphs := paraPattern.Split(content, -1)
	return template.HTML(fmt.Sprintf("<p>%s</p>", strings.Join(paragraphs, "</p><p>")))
}

func paraFilter(content string) string {
	paragraphs := paraPattern.Split(content, -1)
	return fmt.Sprintf("<para>%s</para>", strings.Join(paragraphs, "</para><para>"))
}

func nobrFilter(content string) string {
	normalized := strings.Replace(content, "\r\n", "\n", -1)
	paragraphs := multiNewlinePattern.Split(normalized, -1)
	for i, p := range paragraphs {
		withoutCR := strings.Replace(p, "\r", " ", -1)
		withoutLF := strings.Replace(withoutCR, "\n", " ", -1)
		paragraphs[i] = spacePattern.ReplaceAllString(withoutLF, " ")
	}
	return strings.Join(paragraphs, "\n\n")
}
