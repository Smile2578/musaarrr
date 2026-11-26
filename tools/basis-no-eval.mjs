// Patches three.js's Basis Universal transcoder (web/vendor/basis/basis_transcoder.js) so it runs under
// Musaarr's CSP, which refuses 'unsafe-eval'. Emscripten's embind writes its two kinds of call
// wrappers as source text and compiles them with new Function; here they become plain closures that
// do the same, as Emscripten itself does when built with -sDYNAMIC_EXECUTION=0.
// usage: node tools/basis-no-eval.mjs web/vendor/basis/basis_transcoder.js
import fs from "node:fs";

const file = process.argv[2];
let src = fs.readFileSync(file, "utf8");

// JS → C++: check the count, wire the arguments (this first for a method), call, free, unwire the result.
const invoker = `let[args,invokerFnBody]=createJsInvoker(argTypes,isClassMethodFunc,returns,isAsync);args.push(invokerFnBody);var invokerFn=newFunc(Function,args)(...closureArgs);`;
const invokerFix = `var invokerFn=function(...args){if(args.length!==argCount-2){throwBindingError("function "+humanName+" called with "+args.length+" arguments, expected "+(argCount-2))}` +
  `var destructors=needsDestructorStack?[]:null,wired=[];if(isClassMethodFunc){wired.push(argTypes[1]["toWireType"](destructors,this))}` +
  `for(var i=0;i<argCount-2;++i){wired.push(argTypes[i+2]["toWireType"](destructors,args[i]))}var rv=cppInvokerFunc(cppTargetFunc,...wired);` +
  `if(needsDestructorStack){runDestructors(destructors)}else{for(var i=isClassMethodFunc?1:2;i<argCount;++i){if(argTypes[i].destructorFunction!==null){argTypes[i].destructorFunction(wired[i===1?0:i-2+(isClassMethodFunc?1:0)])}}}` +
  `if(returns){return argTypes[0]["fromWireType"](rv)}};`;

// C++ → JS: read the arguments from the stack, call the method (kind 0), the constructor (1) or the function.
const caller = `params.push(functionBody);var invokerFunction=newFunc(Function,params)(...args);`;
const callerFix = `var invokerFunction=function(obj,func,destructorsRef,argsPtr){var a=kind===0?[obj]:[],offset=0;` +
  `for(var i=0;i<argCount;++i){a.push(types[i].readValueFromPointer(argsPtr+offset));offset+=types[i]["argPackAdvance"]}` +
  `var rv=kind===1?Reflect.construct(func,a):func.call(...a);if(!retType.isVoid){return emval_returnValue(retType,destructorsRef,rv)}};`;

for (const [from, to] of [[invoker, invokerFix], [caller, callerFix]]) {
  if (src.split(from).length !== 2) throw new Error(`not found once in ${file}: ${from.slice(0, 60)}…`);
  src = src.replace(from, () => to);
}
if (/newFunc\(Function/.test(src)) throw new Error("a new Function is left");
fs.writeFileSync(file, src);
