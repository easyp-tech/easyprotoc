package protoc

import (
	"errors"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/easyp-tech/easyprotoc/internal/serialize"
	"github.com/easyp-tech/easyprotoc/parser"
)

// Result represents the successful output of a protoc compilation.
type Result struct {
	wireBytes         []byte
	filesToGen        []string
	allFilesWire      []byte
	orderedFiles      []*parser.ASTFile
	includeSourceInfo bool

	mu            sync.Mutex
	descriptorSet *descriptorpb.FileDescriptorSet
	fileDescs     []protoreflect.FileDescriptor
}

// WireBytes returns the raw wire-encoded FileDescriptorSet matching CLI output.
func (r *Result) WireBytes() []byte {
	return r.wireBytes
}

// DescriptorSet returns the unmarshaled FileDescriptorSet protobuf message.
func (r *Result) DescriptorSet() *descriptorpb.FileDescriptorSet {
	r.mu.Lock()
	defer r.mu.Unlock()
	ds, err := r.descriptorSetLocked()
	if err != nil {
		return nil
	}
	return ds
}

func (r *Result) descriptorSetLocked() (*descriptorpb.FileDescriptorSet, error) {
	if r.descriptorSet != nil {
		return r.descriptorSet, nil
	}
	var ds descriptorpb.FileDescriptorSet
	if err := proto.Unmarshal(r.wireBytes, &ds); err != nil {
		return nil, err
	}
	r.descriptorSet = &ds
	return r.descriptorSet, nil
}

// Files returns protoreflect.FileDescriptor instances for all files in DescriptorSet.
func (r *Result) Files() ([]protoreflect.FileDescriptor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fileDescs != nil {
		return r.fileDescs, nil
	}
	ds, err := r.descriptorSetLocked()
	if err != nil {
		return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
	}
	reg, err := protodesc.NewFiles(ds)
	if err != nil {
		return nil, fmt.Errorf("build file registry: %w", err)
	}
	if types := collectExtensionTypes(reg); types != nil {
		var resolved descriptorpb.FileDescriptorSet
		unmarshalOpts := proto.UnmarshalOptions{
			Resolver: fallbackExtensionResolver{primary: types},
		}
		if err := unmarshalOpts.Unmarshal(r.wireBytes, &resolved); err != nil {
			return nil, fmt.Errorf("resolve option extensions: %w", err)
		}
		reg, err = protodesc.NewFiles(&resolved)
		if err != nil {
			return nil, fmt.Errorf("rebuild file registry: %w", err)
		}
		r.descriptorSet = &resolved
		ds = &resolved
	}
	descs := make([]protoreflect.FileDescriptor, 0, len(ds.GetFile()))
	for _, fdp := range ds.GetFile() {
		fd, err := reg.FindFileByPath(fdp.GetName())
		if err != nil {
			return nil, fmt.Errorf("find file %q: %w", fdp.GetName(), err)
		}
		descs = append(descs, fd)
	}
	r.fileDescs = descs
	return r.fileDescs, nil
}

type fallbackExtensionResolver struct {
	primary *protoregistry.Types
}

func (r fallbackExtensionResolver) FindExtensionByName(field protoreflect.FullName) (protoreflect.ExtensionType, error) {
	if xt, err := r.primary.FindExtensionByName(field); err == nil {
		return xt, nil
	}
	return protoregistry.GlobalTypes.FindExtensionByName(field)
}

func (r fallbackExtensionResolver) FindExtensionByNumber(message protoreflect.FullName, field protoreflect.FieldNumber) (protoreflect.ExtensionType, error) {
	if xt, err := r.primary.FindExtensionByNumber(message, field); err == nil {
		return xt, nil
	}
	return protoregistry.GlobalTypes.FindExtensionByNumber(message, field)
}

func collectExtensionTypes(reg *protoregistry.Files) *protoregistry.Types {
	var types *protoregistry.Types
	register := func(exts protoreflect.ExtensionDescriptors) {
		for i := 0; i < exts.Len(); i++ {
			if types == nil {
				types = new(protoregistry.Types)
			}
			_ = types.RegisterExtension(dynamicpb.NewExtensionType(exts.Get(i)))
		}
	}
	var walkMessages func(msgs protoreflect.MessageDescriptors)
	walkMessages = func(msgs protoreflect.MessageDescriptors) {
		for i := 0; i < msgs.Len(); i++ {
			m := msgs.Get(i)
			register(m.Extensions())
			walkMessages(m.Messages())
		}
	}
	reg.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		register(fd.Extensions())
		walkMessages(fd.Messages())
		return true
	})
	return types
}

// CodeGeneratorRequest builds a CodeGeneratorRequest for plugin execution (REQ-8.9).
func (r *Result) CodeGeneratorRequest(parameter string) (*pluginpb.CodeGeneratorRequest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var allDS *descriptorpb.FileDescriptorSet
	if len(r.allFilesWire) == 0 && len(r.orderedFiles) > 0 {
		s := serialize.NewWireSerializer()
		r.allFilesWire = s.SerializeFileDescriptorSet(r.orderedFiles, r.includeSourceInfo)
	}
	if len(r.allFilesWire) > 0 {
		var ads descriptorpb.FileDescriptorSet
		if err := proto.Unmarshal(r.allFilesWire, &ads); err != nil {
			return nil, fmt.Errorf("failed to unmarshal complete file set: %w", err)
		}
		allDS = &ads
	} else {
		var err error
		allDS, err = r.descriptorSetLocked()
		if err != nil {
			return nil, fmt.Errorf("unmarshal descriptor set: %w", err)
		}
	}

	if allDS == nil {
		return nil, errors.New("no descriptors available")
	}

	genMap := make(map[string]bool, len(r.filesToGen))
	for _, f := range r.filesToGen {
		genMap[f] = true
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: r.filesToGen,
		Parameter:      proto.String(parameter),
		CompilerVersion: &pluginpb.Version{
			Major: proto.Int32(36),
			Minor: proto.Int32(2),
			Patch: proto.Int32(0),
		},
	}

	for _, fdp := range allDS.GetFile() {
		isGen := genMap[fdp.GetName()]
		if isGen {
			// SourceFileDescriptors contains files with all options (including source-retention)
			req.SourceFileDescriptors = append(req.SourceFileDescriptors, proto.Clone(fdp).(*descriptorpb.FileDescriptorProto))

			// ProtoFile contains files with source-retention options stripped for files_to_generate
			protoFile := proto.Clone(fdp).(*descriptorpb.FileDescriptorProto)
			stripSourceRetention(protoFile)
			req.ProtoFile = append(req.ProtoFile, protoFile)
		} else {
			// Dependencies keep source retention options as per protoc spec
			req.ProtoFile = append(req.ProtoFile, proto.Clone(fdp).(*descriptorpb.FileDescriptorProto))
		}
	}

	return req, nil
}

func stripSourceRetention(fdp *descriptorpb.FileDescriptorProto) {
	for _, msg := range fdp.GetMessageType() {
		stripMessageSourceRetention(msg)
	}
	for _, ext := range fdp.GetExtension() {
		stripFieldSourceRetention(ext)
	}
}

func stripMessageSourceRetention(msg *descriptorpb.DescriptorProto) {
	for _, f := range msg.GetField() {
		stripFieldSourceRetention(f)
	}
	for _, nested := range msg.GetNestedType() {
		stripMessageSourceRetention(nested)
	}
	for _, ext := range msg.GetExtension() {
		stripFieldSourceRetention(ext)
	}
	// In ExtensionRangeOptions, declaration is [retention = RETENTION_SOURCE]
	for _, er := range msg.GetExtensionRange() {
		if er.Options != nil {
			er.Options.Declaration = nil
		}
	}
}

func stripFieldSourceRetention(f *descriptorpb.FieldDescriptorProto) {
	// Source-retention field options
}
